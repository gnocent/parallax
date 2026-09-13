package depot

import (
	"errors"
	"strings"
	"testing"
)

// vlanAvecPlages crée un VLAN et ses plages (paires début, fin).
func vlanAvecPlages(t *testing.T, d *Depot, v Vlan, bornes ...string) Vlan {
	t.Helper()
	cree, err := d.CreerVlan(v)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(bornes); i += 2 {
		if _, err := d.CreerPlage(PlageIP{VlanID: cree.ID, IPDebut: bornes[i], IPFin: bornes[i+1]}); err != nil {
			t.Fatal(err)
		}
	}
	relu, err := d.LireVlan(cree.ID)
	if err != nil {
		t.Fatal(err)
	}
	return relu
}

// serveurHypothese insère un serveur HYPOTHESE du scénario, affecté au
// cluster dans le seau du scénario, dans la zone donnée (0 = sans zone).
func serveurHypothese(t *testing.T, c conn, nom string, scenarioID, clusterID, zoneID int64) int64 {
	t.Helper()
	var zone any
	if zoneID != 0 {
		zone = zoneID
	}
	res, err := c.Exec(`INSERT INTO serveur (physical_name, statut, scenario_id, zone_id) VALUES (?, 'HYPOTHESE', ?, ?)`,
		nom, scenarioID, zone)
	if err != nil {
		t.Fatalf("fixture serveur %s : %v", nom, err)
	}
	id, _ := res.LastInsertId()
	if clusterID != 0 {
		exec(t, c, `INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id) VALUES (?, ?, '2027-01-01', ?)`,
			id, clusterID, scenarioID)
	}
	return id
}

func ipServeur(t *testing.T, d *Depot, id int64) (string, *int64) {
	t.Helper()
	s, err := d.LireServeur(id)
	if err != nil {
		t.Fatal(err)
	}
	if s.IP == nil {
		return "", s.VlanID
	}
	return *s.IP, s.VlanID
}

func TestProposerAdresseCurseurEtRebouclage(t *testing.T) {
	d := depotDeTest(t)
	v := vlanAvecPlages(t, d, Vlan{Code: "V"}, "10.0.0.10", "10.0.0.12", "10.0.0.20", "10.0.0.21")
	// les plages sont chargées triées ; on les passe volontairement en désordre
	v.Plages[0], v.Plages[1] = v.Plages[1], v.Plages[0]

	prises := map[string][]int64{}
	sans := func() string { ip, _ := ProposerAdresse(v, prises); return ip }
	if ip := sans(); ip != "10.0.0.10" {
		t.Fatalf("sans curseur : attendu 10.0.0.10, obtenu %s", ip)
	}
	v.DerniereIP = ptrStr("10.0.0.10")
	if ip := sans(); ip != "10.0.0.11" {
		t.Fatalf("après 10.0.0.10 : attendu 10.0.0.11, obtenu %s", ip)
	}
	// fin de la première plage : on passe à la seconde
	v.DerniereIP = ptrStr("10.0.0.12")
	if ip := sans(); ip != "10.0.0.20" {
		t.Fatalf("après 10.0.0.12 : attendu 10.0.0.20, obtenu %s", ip)
	}
	// curseur entre deux plages (adresse forcée) : on reprend à la plage suivante
	v.DerniereIP = ptrStr("10.0.0.15")
	if ip := sans(); ip != "10.0.0.20" {
		t.Fatalf("après 10.0.0.15 : attendu 10.0.0.20, obtenu %s", ip)
	}
	// fin de la dernière plage : rebouclage au début de la première
	v.DerniereIP = ptrStr("10.0.0.21")
	if ip := sans(); ip != "10.0.0.10" {
		t.Fatalf("après 10.0.0.21 : attendu rebouclage sur 10.0.0.10, obtenu %s", ip)
	}
	// curseur au-delà de toutes les plages : rebouclage aussi
	v.DerniereIP = ptrStr("10.0.0.99")
	if ip := sans(); ip != "10.0.0.10" {
		t.Fatalf("après 10.0.0.99 : attendu 10.0.0.10, obtenu %s", ip)
	}
	// curseur invalide : comme sans curseur
	v.DerniereIP = ptrStr("n'importe quoi")
	if ip := sans(); ip != "10.0.0.10" {
		t.Fatalf("curseur invalide : attendu 10.0.0.10, obtenu %s", ip)
	}

	// adresses prises : sautées, rebouclage jusqu'au curseur inclus
	prises = map[string][]int64{"10.0.0.11": {1}, "10.0.0.12": {2}, "10.0.0.20": {3}}
	v.DerniereIP = ptrStr("10.0.0.10")
	if ip := sans(); ip != "10.0.0.21" {
		t.Fatalf("attendu 10.0.0.21 (11, 12 et 20 prises), obtenu %s", ip)
	}
	v.DerniereIP = ptrStr("10.0.0.21")
	if ip := sans(); ip != "10.0.0.10" {
		t.Fatalf("attendu 10.0.0.10 par rebouclage, obtenu %s", ip)
	}
	// tout pris : faux
	prises["10.0.0.10"] = []int64{4}
	prises["10.0.0.21"] = []int64{5}
	if ip, ok := ProposerAdresse(v, prises); ok {
		t.Fatalf("plage épuisée attendue, obtenu %s", ip)
	}
	// VLAN sans plage : faux
	if _, ok := ProposerAdresse(Vlan{Code: "vide"}, map[string][]int64{}); ok {
		t.Fatal("un VLAN sans plage ne propose rien")
	}
}

func TestProposerAdresseTrouLibereReprisAuTourSuivant(t *testing.T) {
	v := Vlan{Code: "V", Plages: []PlageIP{{IPDebut: "10.0.0.1", IPFin: "10.0.0.4"}}}
	prises := map[string][]int64{"10.0.0.1": {1}, "10.0.0.2": {2}, "10.0.0.3": {3}}
	v.DerniereIP = ptrStr("10.0.0.3")
	// .2 est libérée (décommissionnement en cours) : elle n'est pas reprise
	// tant qu'il reste des adresses après le curseur.
	delete(prises, "10.0.0.2")
	ip, _ := ProposerAdresse(v, prises)
	if ip != "10.0.0.4" {
		t.Fatalf("attendu 10.0.0.4 avant de revenir sur le trou, obtenu %s", ip)
	}
	prises["10.0.0.4"] = []int64{4}
	v.DerniereIP = ptrStr("10.0.0.4")
	if ip, _ = ProposerAdresse(v, prises); ip != "10.0.0.2" {
		t.Fatalf("au tour suivant le trou est repris : attendu 10.0.0.2, obtenu %s", ip)
	}
}

func TestAdressesPrisesPoolGlobal(t *testing.T) {
	d := depotDeTest(t)
	actif := scenarioDeTest(t, d.base)
	res, _ := d.base.Exec(`INSERT INTO scenario (nom, description, statut, date_creation, date_cloture)
		VALUES ('Abandonné', 'test', 'ABANDONNE', '2026-01-01', '2026-02-01')`)
	abandonne, _ := res.LastInsertId()

	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, ip) VALUES ('reel', 'EN_SERVICE', '10.0.0.1')`)
	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, ip) VALUES ('commande', 'COMMANDE', ' 10.0.0.2 ')`)
	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, ip) VALUES ('sorti', 'DECOMMISSIONNE', '10.0.0.3')`)
	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, ip, scenario_id) VALUES ('hyp-actif', 'HYPOTHESE', '10.0.0.4', ?)`, actif)
	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, ip, scenario_id) VALUES ('hyp-abandonne', 'HYPOTHESE', '10.0.0.5', ?)`, abandonne)
	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, ip) VALUES ('doublon', 'EN_SERVICE', '10.0.0.1')`)
	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, ip) VALUES ('vide', 'EN_SERVICE', '')`)

	prises, err := d.AdressesPrises()
	if err != nil {
		t.Fatal(err)
	}
	if len(prises) != 3 {
		t.Fatalf("attendu 3 adresses prises (1, 2, 4), obtenu %v", prises)
	}
	if len(prises["10.0.0.1"]) != 2 {
		t.Fatalf("10.0.0.1 portée par deux serveurs, obtenu %v", prises["10.0.0.1"])
	}
	if _, ok := prises["10.0.0.2"]; !ok {
		t.Fatal("l'adresse avec espaces doit être normalisée")
	}
	for _, absente := range []string{"10.0.0.3", "10.0.0.5"} {
		if _, ok := prises[absente]; ok {
			t.Fatalf("%s ne doit pas compter (décommissionné / scénario abandonné)", absente)
		}
	}
}

func TestAdresserScenarioLesQuatreIssues(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	hot := clusterDeTest(t, d.base, r, "ElasticHot")
	kafka := clusterDeTest(t, d.base, r, "Kafka")
	res, _ := d.base.Exec(`INSERT INTO cluster (nom, projet_id, environnement_id, techno_id) VALUES ('Qual', ?, ?, ?)`,
		r.Projet, r.EnvQual, r.TechnoElastic)
	qual, _ := res.LastInsertId()
	sc := scenarioDeTest(t, d.base)

	// PROD/DC1 : un seul VLAN, deux adresses ; PROD/DC2 : deux VLAN candidats ;
	// Kafka : un VLAN dont l'unique adresse est déjà prise par un serveur réel.
	vlanDC1 := vlanAvecPlages(t, d, Vlan{Code: "PROD-DC1", EnvironnementID: ptrI64(r.EnvProd), ZoneID: ptrI64(r.DC1)}, "10.1.0.10", "10.1.0.11")
	vlanAvecPlages(t, d, Vlan{Code: "PROD-DC2-A", EnvironnementID: ptrI64(r.EnvProd), ZoneID: ptrI64(r.DC2)}, "10.2.0.10", "10.2.0.20")
	vlanAvecPlages(t, d, Vlan{Code: "PROD-DC2-B", EnvironnementID: ptrI64(r.EnvProd), ZoneID: ptrI64(r.DC2)}, "10.3.0.10", "10.3.0.20")
	vlanKafka := vlanAvecPlages(t, d, Vlan{Code: "KAFKA", ClusterID: ptrI64(kafka)}, "10.9.0.1", "10.9.0.1")
	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, ip, vlan_id) VALUES ('reel-kafka', 'EN_SERVICE', '10.9.0.1', ?)`, vlanKafka.ID)

	s1 := serveurHypothese(t, d.base, "hot-01", sc, hot, r.DC1)
	s2 := serveurHypothese(t, d.base, "hot-02", sc, hot, r.DC1)
	s3 := serveurHypothese(t, d.base, "hot-03", sc, hot, r.DC1)             // plage épuisée après s1 et s2
	s4 := serveurHypothese(t, d.base, "hot-dc2", sc, hot, r.DC2)            // à choisir
	s5 := serveurHypothese(t, d.base, "qual-01", sc, qual, r.DC1)           // aucun VLAN applicable
	s6 := serveurHypothese(t, d.base, "kafka-01", sc, kafka, 0)             // plage épuisée par le réel
	s7 := serveurHypothese(t, d.base, "orphelin", sc, 0, r.DC1)             // sans affectation
	exec(t, d.base, `UPDATE serveur SET ip = '10.1.0.99' WHERE id = ?`, s2) // déjà adressé, non touché
	// serveur sur Qual (aucun VLAN applicable) mais VLAN PROD-DC1 posé à la
	// main : le VLAN prime sur la déduction. Traité après ElasticHot dans
	// l'ordre (cluster Qual), il trouve la plage épuisée — pas « sans VLAN ».
	s8 := serveurHypothese(t, d.base, "hot-force", sc, qual, r.DC1)
	exec(t, d.base, `UPDATE serveur SET vlan_id = ? WHERE id = ?`, vlanDC1.ID, s8)

	// la simulation ne change rien
	sim, err := d.SimulerAdressage(sc)
	if err != nil {
		t.Fatal(err)
	}
	if sim.Comptes[AdressageAttribue] != 2 {
		t.Fatalf("simulation : 2 attributions attendues, obtenu %v", sim.Comptes)
	}
	if ip, _ := ipServeur(t, d, s1); ip != "" {
		t.Fatal("la simulation ne doit rien écrire")
	}

	resAdr, err := d.AdresserScenario(sc)
	if err != nil {
		t.Fatal(err)
	}
	parServeur := map[int64]LigneAdressage{}
	for _, l := range resAdr.Lignes {
		parServeur[l.ServeurID] = l
	}
	attendu := map[int64]string{
		s1: AdressageAttribue, s2: AdressageDejaAdresse, s3: AdressageAttribue,
		s4: AdressageAChoisir, s5: AdressageSansVlan, s6: AdressagePlageEpuisee,
		s7: AdressageSansVlan, s8: AdressagePlageEpuisee,
	}
	for id, issue := range attendu {
		if parServeur[id].Issue != issue {
			t.Errorf("serveur %d (%s) : issue attendue %s, obtenu %+v", id, parServeur[id].PhysicalName, issue, parServeur[id])
		}
	}
	if len(resAdr.Lignes) != 8 {
		t.Fatalf("8 lignes attendues, obtenu %d", len(resAdr.Lignes))
	}
	// ordre stable : cluster puis nom — les sans-cluster (NULL) d'abord dans SQLite
	if resAdr.Lignes[0].PhysicalName != "orphelin" || resAdr.Lignes[1].PhysicalName != "hot-01" {
		t.Fatalf("ordre attendu orphelin, hot-01, … ; obtenu %s, %s", resAdr.Lignes[0].PhysicalName, resAdr.Lignes[1].PhysicalName)
	}

	// PROD-DC1 n'a que .10 et .11 : hot-01 puis hot-03 (hot-02 porte déjà
	// 10.1.0.99, hors plage mais dans le pool) ; hot-force, traité après, ne
	// trouve plus rien.
	ip1, vlan1 := ipServeur(t, d, s1)
	if ip1 != "10.1.0.10" || vlan1 == nil || *vlan1 != vlanDC1.ID {
		t.Fatalf("hot-01 : attendu 10.1.0.10 dans PROD-DC1, obtenu %s / %v", ip1, vlan1)
	}
	if ip3, _ := ipServeur(t, d, s3); ip3 != "10.1.0.11" {
		t.Fatalf("hot-03 : attendu 10.1.0.11, obtenu %q", ip3)
	}
	if ip8, _ := ipServeur(t, d, s8); ip8 != "" || parServeur[s8].VlanCode != "PROD-DC1" {
		t.Fatalf("hot-force : plage épuisée dans PROD-DC1 attendue, obtenu %q / %+v", ip8, parServeur[s8])
	}
	if ip4, vlan4 := ipServeur(t, d, s4); ip4 != "" || vlan4 != nil {
		t.Fatal("un serveur à choisir ne doit pas être touché")
	}
	if l := parServeur[s4]; len(l.Candidats) != 2 || !strings.Contains(l.Detail, "PROD-DC2-A") {
		t.Fatalf("candidats attendus PROD-DC2-A et PROD-DC2-B : %+v", l)
	}
	if l := parServeur[s2]; l.IP != "10.1.0.99" {
		t.Fatalf("déjà adressé : l'adresse existante est rappelée, obtenu %+v", l)
	}
	if l := parServeur[s6]; l.VlanCode != "KAFKA" {
		t.Fatalf("plage épuisée : le VLAN est nommé, obtenu %+v", l)
	}
	if v, _ := d.LireVlan(vlanDC1.ID); v.DerniereIP == nil || *v.DerniereIP != "10.1.0.11" {
		t.Fatalf("curseur attendu 10.1.0.11, obtenu %v", v.DerniereIP)
	}

	// second appel : rien de nouveau à attribuer, aucune adresse redonnée
	bis, err := d.AdresserScenario(sc)
	if err != nil {
		t.Fatal(err)
	}
	if bis.Comptes[AdressageAttribue] != 0 || bis.Comptes[AdressageDejaAdresse] != 3 {
		t.Fatalf("second appel : 0 attribution et 3 déjà adressés attendus, obtenu %v", bis.Comptes)
	}

	// le cas « à choisir » se règle serveur par serveur
	vB, _ := d.LireVlanParCode("PROD-DC2-B")
	ip, err := d.AdresserServeur(s4, vB.ID)
	if err != nil || ip != "10.3.0.10" {
		t.Fatalf("AdresserServeur : attendu 10.3.0.10, obtenu %s, %v", ip, err)
	}
	if _, err := d.AdresserServeur(s4, vB.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("déjà adressé : ErrValidation attendue, obtenu %v", err)
	}
	if _, err := d.AdresserServeur(s6, vlanKafka.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("plage épuisée : ErrValidation attendue, obtenu %v", err)
	}
	if _, err := d.AdresserServeur(999, vB.ID); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("serveur inconnu : ErrIntrouvable attendue, obtenu %v", err)
	}

	// scénario inconnu / abandonné
	if _, err := d.AdresserScenario(999); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("scénario inconnu : ErrIntrouvable attendue, obtenu %v", err)
	}
	exec(t, d.base, `UPDATE scenario SET statut = 'ABANDONNE' WHERE id = ?`, sc)
	if _, err := d.AdresserScenario(sc); !errors.Is(err, ErrValidation) {
		t.Fatalf("scénario abandonné : ErrValidation attendue, obtenu %v", err)
	}
}

func TestAdresserDeuxScenariosPoolGlobal(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	hot := clusterDeTest(t, d.base, r, "ElasticHot")
	vlanAvecPlages(t, d, Vlan{Code: "PROD"}, "10.0.0.1", "10.0.0.3")

	scA := scenarioDeTest(t, d.base)
	scB := scenarioDeTest(t, d.base)
	a1 := serveurHypothese(t, d.base, "a-01", scA, hot, r.DC1)
	a2 := serveurHypothese(t, d.base, "a-02", scA, hot, r.DC1)
	b1 := serveurHypothese(t, d.base, "b-01", scB, hot, r.DC1)
	b2 := serveurHypothese(t, d.base, "b-02", scB, hot, r.DC1)

	if _, err := d.AdresserScenario(scA); err != nil {
		t.Fatal(err)
	}
	resB, err := d.AdresserScenario(scB)
	if err != nil {
		t.Fatal(err)
	}
	vues := map[string]int64{}
	for _, id := range []int64{a1, a2, b1} {
		ip, _ := ipServeur(t, d, id)
		if ip == "" {
			t.Fatalf("serveur %d sans adresse", id)
		}
		if autre, deja := vues[ip]; deja {
			t.Fatalf("adresse %s donnée deux fois (serveurs %d et %d)", ip, autre, id)
		}
		vues[ip] = id
	}
	if ip, _ := ipServeur(t, d, b2); ip != "" || resB.Comptes[AdressagePlageEpuisee] != 1 {
		t.Fatalf("b-02 : plage épuisée attendue (3 adresses pour 4 serveurs), obtenu %q, %v", ip, resB.Comptes)
	}
}

func TestAnomaliesReseauChaqueType(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	hot := clusterDeTest(t, d.base, r, "ElasticHot")
	res, _ := d.base.Exec(`INSERT INTO cluster (nom, projet_id, environnement_id, techno_id) VALUES ('Qual', ?, ?, ?)`,
		r.Projet, r.EnvQual, r.TechnoElastic)
	qual, _ := res.LastInsertId()

	vProd := vlanAvecPlages(t, d, Vlan{Code: "PROD", EnvironnementID: ptrI64(r.EnvProd)}, "10.0.0.1", "10.0.0.50")
	vDC2 := vlanAvecPlages(t, d, Vlan{Code: "DC2", ZoneID: ptrI64(r.DC2)}, "10.2.0.1", "10.2.0.50")
	vSansPlage, _ := d.CreerVlan(Vlan{Code: "SANS-PLAGE"})

	reel := func(nom, ip string, vlanID *int64, zoneID *int64, clusterID int64, statut string) int64 {
		t.Helper()
		res, err := d.base.Exec(`INSERT INTO serveur (physical_name, statut, ip, vlan_id, zone_id) VALUES (?, ?, ?, ?, ?)`,
			nom, statut, ip, vlanID, zoneID)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if clusterID != 0 {
			exec(t, d.base, `INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (?, ?, '2026-01-01')`, id, clusterID)
		}
		return id
	}

	ok := reel("ok", "10.0.0.5", &vProd.ID, ptrI64(r.DC1), hot, "EN_SERVICE")
	dup1 := reel("dup-1", "10.0.0.7", &vProd.ID, ptrI64(r.DC1), hot, "EN_SERVICE")
	dup2 := reel("dup-2", " 10.0.0.7 ", &vProd.ID, ptrI64(r.DC1), hot, "COMMANDE")
	reel("dup-sorti", "10.0.0.7", &vProd.ID, ptrI64(r.DC1), hot, "DECOMMISSIONNE") // ne compte pas
	horsPlage := reel("hors-plage", "10.0.0.200", &vProd.ID, ptrI64(r.DC1), hot, "EN_SERVICE")
	sansVlan := reel("sans-vlan", "10.0.0.9", nil, ptrI64(r.DC1), hot, "EN_SERVICE")
	invalide := reel("invalide", "10.0.0.x", &vProd.ID, ptrI64(r.DC1), hot, "EN_SERVICE")
	incompatible := reel("incompatible", "10.0.0.10", &vProd.ID, ptrI64(r.DC1), qual, "EN_SERVICE") // PROD sur un cluster QUAL
	mauvaiseZone := reel("mauvaise-zone", "10.2.0.3", &vDC2.ID, ptrI64(r.DC1), 0, "EN_SERVICE")     // sans affectation : zone seule
	sansPlage := reel("sans-plage", "10.5.0.1", &vSansPlage.ID, nil, hot, "EN_SERVICE")             // VLAN sans plage : pas hors plage
	reel("vlan-seul", "", &vProd.ID, ptrI64(r.DC1), hot, "EN_SERVICE")                              // VLAN sans adresse : rien

	anomalies, err := d.AnomaliesReseau()
	if err != nil {
		t.Fatal(err)
	}
	parServeur := map[int64][]string{}
	details := map[int64]string{}
	for _, a := range anomalies {
		parServeur[a.ServeurID] = append(parServeur[a.ServeurID], a.Type)
		details[a.ServeurID] += a.Detail + " | "
	}
	attendu := map[int64][]string{
		ok:           nil,
		dup1:         {AnomalieAdresseMultiple},
		dup2:         {AnomalieAdresseMultiple},
		horsPlage:    {AnomalieHorsPlage},
		sansVlan:     {AnomalieSansVlan},
		invalide:     {AnomalieIPInvalide},
		incompatible: {AnomalieVlanIncompatible},
		mauvaiseZone: {AnomalieVlanIncompatible},
		sansPlage:    nil,
	}
	for id, types := range attendu {
		if strings.Join(parServeur[id], ",") != strings.Join(types, ",") {
			t.Errorf("serveur %d : attendu %v, obtenu %v (%s)", id, types, parServeur[id], details[id])
		}
	}
	if len(anomalies) != 7 {
		t.Fatalf("7 anomalies attendues, obtenu %d : %+v", len(anomalies), anomalies)
	}
	if !strings.Contains(details[dup1], "dup-2") || strings.Contains(details[dup1], "dup-sorti") {
		t.Fatalf("dup-1 doit nommer dup-2 et pas le décommissionné : %s", details[dup1])
	}
	if !strings.Contains(details[horsPlage], "10.0.0.1–10.0.0.50") {
		t.Fatalf("hors plage doit rappeler les plages : %s", details[horsPlage])
	}
	// tri par nom de serveur
	for i := 1; i < len(anomalies); i++ {
		if anomalies[i-1].PhysicalName > anomalies[i].PhysicalName {
			t.Fatalf("anomalies non triées : %s après %s", anomalies[i].PhysicalName, anomalies[i-1].PhysicalName)
		}
	}
}
