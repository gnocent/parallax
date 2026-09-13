package depot

import (
	"errors"
	"testing"
)

// semerDemandes construit un parc représentatif pour les fiches de demande :
// un cluster complet (tier, usage), une révision avec composants canoniques,
// un VLAN, un serveur réel entièrement renseigné, un serveur réel nu (sans
// affectation ni révision), et un serveur HYPOTHESE affecté dans son
// scénario. Renvoie (réel complet, réel nu, hypothèse, scénario).
func semerDemandes(t *testing.T, d *Depot) (complet, nu, hypothese, scenario int64) {
	t.Helper()
	r := semerRefs(t, d.base)
	exec(t, d.base, `INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id, usage_fonctionnel_id)
		VALUES (1, 'ElasticCold1', ?, ?, ?, ?, ?)`, r.Projet, r.EnvProd, r.TechnoElastic, r.TierCold, r.UsageLog)
	exec(t, d.base, `INSERT INTO modele (id, type, annee, code, description)
		VALUES (1, 'DENSE', 2025, 'DENSE-2025', 'Stockage dense')`)
	exec(t, d.base, `INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (1, 1, 2, '2025-01-15')`)
	exec(t, d.base, `INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite) VALUES
		(1, 'CPU', 'cpu', 1, 64, 'CORE'),
		(1, 'RAM', 'ram', 1, 1024, 'GO'),
		(1, 'DISQUE_DATA', 'ssd', 24, 7.68, 'TO'),
		(1, 'NIC', 'nic', 2, 25, 'GBPS'),
		(1, 'GPU', 'gpu', 2, 1, 'UNITE'),
		(1, 'GPU', 'gpu_ram', 2, 80, 'GO')`)
	exec(t, d.base, `INSERT INTO vlan (id, code) VALUES (1, 'VL-PROD-DC1')`)
	exec(t, d.base, `UPDATE zone SET site = 'Paris' WHERE id = ?`, r.DC1)

	s, err := d.CreerServeur(Serveur{
		PhysicalName: ptrStr("PHY001"), Hostname: ptrStr("esh01"), IP: ptrStr("10.0.0.1"),
		ZoneID: &r.DC1, PositionZone: ptrStr("B12"), VlanID: ptrI64(1),
		Typologie: ptrStr("PA"), CodeAppli: ptrStr("APP1"), Commentaire: ptrStr("noyau"),
		DemandeRef: ptrStr("D-100"), DemandeServeurRef: ptrStr("F-1"),
		Statut: StatutEnService, DateEntree: ptrStr("2025-03-01"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.RattacherRevision(s.ID, 1, "2025-03-01", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(s.ID, 1, "2025-03-01", nil, nil); err != nil {
		t.Fatal(err)
	}

	n, err := d.CreerServeur(Serveur{PhysicalName: ptrStr("PHY002"), Statut: StatutCommande})
	if err != nil {
		t.Fatal(err)
	}

	sc := scenarioDeTest(t, d.base)
	h, err := d.CreerServeur(Serveur{
		PhysicalName: ptrStr("HYP001"), Statut: StatutHypothese, ScenarioID: &sc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.RattacherRevision(h.ID, 1, "2027-01-01", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(h.ID, 1, "2027-01-01", &sc, nil); err != nil {
		t.Fatal(err)
	}
	return s.ID, n.ID, h.ID, sc
}

func TestFicheDemandeResolutionComplete(t *testing.T) {
	d := depotDeTest(t)
	complet, _, _, _ := semerDemandes(t, d)

	f, err := d.LireFicheDemande(complet)
	if err != nil {
		t.Fatal(err)
	}
	attendu := map[string]string{
		"num_fiche": "F-1", "num_demande": "D-100",
		"nom_physique": "PHY001", "hostname": "esh01", "ip": "10.0.0.1",
		"vlan": "VL-PROD-DC1", "zone": "DC1", "site": "Paris", "position": "B12",
		"typologie": "PA", "code_appli": "APP1", "commentaire": "noyau",
		"projet": "LOGS", "environnement": "PROD", "techno": "ELASTIC",
		"tier": "COLD", "usage": "LOGMGMT", "cluster": "ElasticCold1",
		"modele": "DENSE-2025", "modele_type": "DENSE", "modele_description": "Stockage dense",
		"revision": "2",
		"cpu":      "64", "ram": "1024", "hdd": "", "ssd": "184.32", "nic": "50", "gpu": "2",
		"statut": "EN_SERVICE", "date_entree": "2025-03-01", "scenario": "",
	}
	valeurs := f.Valeurs()
	for cle, v := range attendu {
		if valeurs[cle] != v {
			t.Errorf("%s : attendu %q, obtenu %q", cle, v, valeurs[cle])
		}
	}
	if len(valeurs) != len(attendu) {
		t.Errorf("le catalogue de variables a %d entrées, le test en attend %d", len(valeurs), len(attendu))
	}
}

func TestFicheDemandeServeurNuEtHypothese(t *testing.T) {
	d := depotDeTest(t)
	_, nu, hypothese, sc := semerDemandes(t, d)

	f, err := d.LireFicheDemande(nu)
	if err != nil {
		t.Fatal(err)
	}
	if f.NomPhysique != "PHY002" || f.Statut != StatutCommande {
		t.Fatalf("fiche du serveur nu inattendue : %+v", f)
	}
	for cle, v := range f.Valeurs() {
		if cle == "nom_physique" || cle == "statut" {
			continue
		}
		if v != "" {
			t.Errorf("serveur nu : %s devrait être vide, obtenu %q", cle, v)
		}
	}

	// l'hypothèse prend son affectation dans le seau de son scénario, et
	// porte le nom du scénario.
	fh, err := d.LireFicheDemande(hypothese)
	if err != nil {
		t.Fatal(err)
	}
	if fh.Cluster != "ElasticCold1" || fh.Scenario != "Hypothèse 2027" || fh.Modele != "DENSE-2025" {
		t.Fatalf("fiche de l'hypothèse inattendue : %+v", fh)
	}
	_ = sc

	if _, err := d.LireFicheDemande(9999); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("serveur inexistant : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestListerFichesDemandeFiltresEtTri(t *testing.T) {
	d := depotDeTest(t)
	complet, nu, hypothese, sc := semerDemandes(t, d)

	ids := func(fs []FicheDemande) []int64 {
		out := make([]int64, len(fs))
		for i, f := range fs {
			out[i] = f.ServeurID
		}
		return out
	}
	egal := func(a, b []int64) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	// réel : le serveur affecté d'abord (tri par cluster), le nu ensuite.
	reel, err := d.ListerFichesDemande(FiltreDemande{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(reel); !egal(got, []int64{complet, nu}) {
		t.Fatalf("réel : attendu [%d %d], obtenu %v", complet, nu, got)
	}

	// scénario : uniquement ses hypothèses, jamais le réel superposé.
	hyp, err := d.ListerFichesDemande(FiltreDemande{ScenarioID: &sc})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(hyp); !egal(got, []int64{hypothese}) {
		t.Fatalf("scénario : attendu [%d], obtenu %v", hypothese, got)
	}

	// périmètre : un serveur sans affectation n'a pas de projet.
	projet := int64(1)
	parProjet, _ := d.ListerFichesDemande(FiltreDemande{ProjetID: &projet})
	if got := ids(parProjet); !egal(got, []int64{complet}) {
		t.Fatalf("filtre projet : attendu [%d], obtenu %v", complet, got)
	}
	cluster := int64(1)
	parCluster, _ := d.ListerFichesDemande(FiltreDemande{ClusterID: &cluster})
	if got := ids(parCluster); !egal(got, []int64{complet}) {
		t.Fatalf("filtre cluster : attendu [%d], obtenu %v", complet, got)
	}
	env := int64(2)
	parEnv, _ := d.ListerFichesDemande(FiltreDemande{EnvironnementID: &env})
	if len(parEnv) != 0 {
		t.Fatalf("filtre environnement QUAL : attendu vide, obtenu %v", ids(parEnv))
	}

	statut := StatutCommande
	parStatut, _ := d.ListerFichesDemande(FiltreDemande{Statut: &statut})
	if got := ids(parStatut); !egal(got, []int64{nu}) {
		t.Fatalf("filtre statut : attendu [%d], obtenu %v", nu, got)
	}

	ref := "D-100"
	parRef, _ := d.ListerFichesDemande(FiltreDemande{DemandeRef: &ref})
	if got := ids(parRef); !egal(got, []int64{complet}) {
		t.Fatalf("filtre numéro : attendu [%d], obtenu %v", complet, got)
	}
	sans, _ := d.ListerFichesDemande(FiltreDemande{SansNumero: true})
	if got := ids(sans); !egal(got, []int64{nu}) {
		t.Fatalf("sans numéro : attendu [%d], obtenu %v", nu, got)
	}
}

func TestDefinirDemandeRefEnMasse(t *testing.T) {
	d := depotDeTest(t)
	complet, nu, hypothese, _ := semerDemandes(t, d)

	n, err := d.DefinirDemandeRef([]int64{nu, hypothese, 9999}, ptrStr(" D-200 "))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("attendu 2 serveurs touchés (l'identifiant inconnu est ignoré), obtenu %d", n)
	}
	for _, id := range []int64{nu, hypothese} {
		s, _ := d.LireServeur(id)
		if s.DemandeRef == nil || *s.DemandeRef != "D-200" {
			t.Fatalf("serveur %d : numéro attendu « D-200 » (recadré), obtenu %v", id, s.DemandeRef)
		}
	}
	// le serveur non listé n'est pas touché.
	s, _ := d.LireServeur(complet)
	if s.DemandeRef == nil || *s.DemandeRef != "D-100" {
		t.Fatalf("le serveur hors liste ne doit pas changer, obtenu %v", s.DemandeRef)
	}

	// vide → NULL, et une liste vide ne fait rien.
	if _, err := d.DefinirDemandeRef([]int64{complet}, ptrStr("   ")); err != nil {
		t.Fatal(err)
	}
	s, _ = d.LireServeur(complet)
	if s.DemandeRef != nil {
		t.Fatalf("une référence blanche doit devenir NULL, obtenu %q", *s.DemandeRef)
	}
	if n, err := d.DefinirDemandeRef(nil, ptrStr("D-300")); err != nil || n != 0 {
		t.Fatalf("liste vide : attendu 0 sans erreur, obtenu %d %v", n, err)
	}
}

func TestDefinirDemandeServeurRef(t *testing.T) {
	d := depotDeTest(t)
	_, nu, _, _ := semerDemandes(t, d)

	if err := d.DefinirDemandeServeurRef(nu, ptrStr("F-7")); err != nil {
		t.Fatal(err)
	}
	f, _ := d.LireFicheDemande(nu)
	if f.NumFiche != "F-7" {
		t.Fatalf("attendu « F-7 », obtenu %q", f.NumFiche)
	}
	if err := d.DefinirDemandeServeurRef(nu, ptrStr("")); err != nil {
		t.Fatal(err)
	}
	s, _ := d.LireServeur(nu)
	if s.DemandeServeurRef != nil {
		t.Fatalf("une référence vide doit devenir NULL, obtenu %q", *s.DemandeServeurRef)
	}
	if err := d.DefinirDemandeServeurRef(9999, ptrStr("F-8")); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("serveur inexistant : attendu ErrIntrouvable, obtenu %v", err)
	}
}
