package depot

import (
	"errors"
	"testing"
)

// deuxClusters crée un cluster dans le projet 1 et un dans le projet 2, et
// renvoie leurs identifiants.
func deuxClusters(t *testing.T, d *Depot, r refs) (int64, int64) {
	t.Helper()
	a, err := d.CreerCluster(Cluster{
		Nom: "ElasticHot", ProjetID: r.Projet, EnvironnementID: r.EnvProd, TechnoID: r.TechnoElastic,
	})
	if err != nil {
		t.Fatalf("cluster A : %v", err)
	}
	b, err := d.CreerCluster(Cluster{
		Nom: "Stream", ProjetID: r.Projet2, EnvironnementID: r.EnvProd, TechnoID: r.TechnoKafka,
	})
	if err != nil {
		t.Fatalf("cluster B : %v", err)
	}
	return a.ID, b.ID
}

func serveurAffectDeTest(t *testing.T) (*Depot, refs, int64) {
	t.Helper()
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	s, err := d.CreerServeur(Serveur{PhysicalName: ptrStr("PHY"), Statut: StatutEnService})
	if err != nil {
		t.Fatalf("serveur : %v", err)
	}
	return d, r, s.ID
}

func TestAffecterEtDesaffecter(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, _ := deuxClusters(t, d, r)

	if err := d.Affecter(srv, clA, "2020-01-01", nil, ptrStr("mise en service")); err != nil {
		t.Fatalf("affectation : %v", err)
	}
	list, _ := d.ListerAffectationsServeur(srv)
	if len(list) != 1 || list[0].DateFin != nil {
		t.Fatalf("affectation active attendue : %+v", list)
	}

	if err := d.Desaffecter(list[0].ID, "2019-01-01"); !errors.Is(err, ErrValidation) {
		t.Fatalf("fin antérieure au début : attendu ErrValidation, obtenu %v", err)
	}
	if err := d.Desaffecter(list[0].ID, "2023-12-31"); err != nil {
		t.Fatalf("désaffectation : %v", err)
	}
	list, _ = d.ListerAffectationsServeur(srv)
	if list[0].DateFin == nil || *list[0].DateFin != "2023-12-31" {
		t.Fatalf("clôture non persistée : %+v", list[0])
	}
	if err := d.Desaffecter(list[0].ID, "2024-01-01"); !errors.Is(err, ErrValidation) {
		t.Fatalf("affectation déjà close : attendu ErrValidation, obtenu %v", err)
	}
}

func TestAffecterSecondeActiveRefusee(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r)

	if err := d.Affecter(srv, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(srv, clB, "2021-01-01", nil, nil); !errors.Is(err, ErrChevauchement) {
		t.Fatalf("seconde affectation active dans le réel : attendu ErrChevauchement, obtenu %v", err)
	}
}

func TestAffecterChevauchementFerme(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r)

	if err := d.Affecter(srv, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Desaffecter(mustActive(t, d, srv), "2024-06-30"); err != nil {
		t.Fatal(err)
	}
	// nouvelle période démarrant avant la fin de la précédente
	if err := d.Affecter(srv, clB, "2024-01-01", nil, nil); !errors.Is(err, ErrChevauchement) {
		t.Fatalf("recouvrement d'une période fermée : attendu ErrChevauchement, obtenu %v", err)
	}
	// après la fin de la précédente : accepté
	if err := d.Affecter(srv, clB, "2024-07-01", nil, nil); err != nil {
		t.Fatalf("période disjointe : %v", err)
	}
}

func TestAffecterSeauScenarioIndependant(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r)
	sc := scenarioDeTest(t, d.base)

	if err := d.Affecter(srv, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatalf("affectation réelle : %v", err)
	}
	// même serveur, affectation active en parallèle dans le seau du scénario
	if err := d.Affecter(srv, clB, "2020-01-01", &sc, nil); err != nil {
		t.Fatalf("affectation active du scénario en parallèle du réel : %v", err)
	}
	// mais une seconde active dans le seau du scénario est refusée
	if err := d.Affecter(srv, clA, "2021-01-01", &sc, nil); !errors.Is(err, ErrChevauchement) {
		t.Fatalf("seconde active dans le seau scénario : attendu ErrChevauchement, obtenu %v", err)
	}
}

func TestReaffecterConserveHistorique(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r) // clA projet 1, clB projet 2

	if err := d.Affecter(srv, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Reaffecter(srv, clB, "2024-01-01", nil, ptrStr("fin de lease, bascule projet")); err != nil {
		t.Fatalf("réaffectation : %v", err)
	}

	list, err := d.ListerAffectationsServeur(srv)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("l'historique complet doit être conservé (2 lignes) : %+v", list)
	}
	if list[0].ClusterID != clA || list[0].DateFin == nil || *list[0].DateFin != "2023-12-31" {
		t.Fatalf("1re vie : cluster A, fermée à J-1 (2023-12-31) : %+v", list[0])
	}
	if list[1].ClusterID != clB || list[1].DateFin != nil || list[1].DateDebut != "2024-01-01" {
		t.Fatalf("2e vie : cluster B, ouverte au 2024-01-01 : %+v", list[1])
	}

	// projets distincts via l'historique enrichi
	vies, _ := d.HistoriqueServeur(srv, nil)
	if len(vies) != 2 || vies[0].ClusterProjetID != r.Projet || vies[1].ClusterProjetID != r.Projet2 {
		t.Fatalf("réaffectation inter-projets non reflétée : %+v", vies)
	}

	// réaffectation sans affectation courante
	autre, _ := d.CreerServeur(Serveur{Statut: StatutEnService})
	if err := d.Reaffecter(autre.ID, clA, "2024-01-01", nil, nil); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("aucune affectation active : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestHistoriqueServeurEtCluster(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r)
	sc := scenarioDeTest(t, d.base)

	if err := d.Affecter(srv, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	// surcharge du scénario : le serveur rejoint clB dans le scénario dès 2027
	if err := d.Affecter(srv, clB, "2027-01-01", &sc, nil); err != nil {
		t.Fatal(err)
	}

	// ListerAffectationsCluster : réel seul vs réel + scénario
	reel, _ := d.ListerAffectationsCluster(clA, nil, "2026-01-01")
	if len(reel) != 1 {
		t.Fatalf("cluster A au 2026 (réel) : %+v", reel)
	}
	avecSc, _ := d.ListerAffectationsCluster(clB, &sc, "2027-06-01")
	if len(avecSc) != 1 || avecSc[0].ScenarioID == nil {
		t.Fatalf("cluster B au 2027 (scénario) : %+v", avecSc)
	}
	sansSc, _ := d.ListerAffectationsCluster(clB, nil, "2027-06-01")
	if len(sansSc) != 0 {
		t.Fatalf("cluster B n'a aucune affectation réelle : %+v", sansSc)
	}
	avant, _ := d.ListerAffectationsCluster(clA, nil, "2019-01-01")
	if len(avant) != 0 {
		t.Fatalf("aucune affectation avant le début : %+v", avant)
	}

	// HistoriqueServeur avec surcharge scénario : les deux vies
	vies, _ := d.HistoriqueServeur(srv, &sc)
	if len(vies) != 2 {
		t.Fatalf("historique scénario attendu 2 vies : %+v", vies)
	}
}

// TestAffecterDansScenario couvre le geste « déplacer » : première ligne du
// scénario pour un serveur réel (rien à clôturer), puis un second
// déplacement à l'intérieur du même scénario (clôture puis ouverture, comme
// Reaffecter mais dans le seau du scénario).
func TestAffecterDansScenario(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r)
	sc := scenarioDeTest(t, d.base)

	if err := d.Affecter(srv, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}

	// premier déplacement dans le scénario : le réel n'est pas touché.
	if err := d.AffecterDansScenario(srv, clB, "2026-01-01", sc, nil); err != nil {
		t.Fatalf("premier déplacement : %v", err)
	}
	reel, _ := d.ListerAffectationsCluster(clA, nil, "2026-06-01")
	if len(reel) != 1 {
		t.Fatalf("le réel doit rester sur clA : %+v", reel)
	}
	sousSc, _ := d.AffectationsResolues(clA, &sc, "2026-06-01")
	if len(sousSc) != 0 {
		t.Fatalf("sous le scénario, clA ne doit plus porter ce serveur : %+v", sousSc)
	}
	sousScB, _ := d.AffectationsResolues(clB, &sc, "2026-06-01")
	if len(sousScB) != 1 {
		t.Fatalf("sous le scénario, clB doit porter ce serveur : %+v", sousScB)
	}

	// second déplacement, à l'intérieur du même scénario : retour sur clA.
	if err := d.AffecterDansScenario(srv, clA, "2026-07-01", sc, nil); err != nil {
		t.Fatalf("second déplacement : %v", err)
	}
	sousScA2, _ := d.AffectationsResolues(clA, &sc, "2026-08-01")
	if len(sousScA2) != 1 {
		t.Fatalf("le second déplacement doit ramener le serveur sur clA : %+v", sousScA2)
	}
	vies, _ := d.ListerAffectationsServeur(srv)
	if len(vies) != 3 { // réel + deux lignes de scénario (clB close, clA ouverte)
		t.Fatalf("attendu 3 lignes d'affectation au total : %+v", vies)
	}

	// date non postérieure à l'affectation courante du scénario : refusé.
	if err := d.AffecterDansScenario(srv, clB, "2026-07-01", sc, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("date non postérieure : attendu ErrValidation, obtenu %v", err)
	}
}

// TestRetirerDuScenario couvre le geste « retirer » : première fois (calque
// sur le réel, puis clôture) et retrait répété (la ligne du scénario existe
// déjà, seule sa date de fin doit bouger).
func TestRetirerDuScenario(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, _ := deuxClusters(t, d, r)
	sc := scenarioDeTest(t, d.base)

	// sans aucune affectation réelle ni de scénario : rien à retirer.
	if err := d.RetirerDuScenario(srv, sc, "2026-01-01"); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("rien à retirer : attendu ErrIntrouvable, obtenu %v", err)
	}

	if err := d.Affecter(srv, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.RetirerDuScenario(srv, sc, "2026-01-01"); err != nil {
		t.Fatalf("premier retrait : %v", err)
	}

	reel, _ := d.ListerAffectationsCluster(clA, nil, "2026-06-01")
	if len(reel) != 1 {
		t.Fatalf("le réel doit rester inchangé : %+v", reel)
	}
	sousSc, _ := d.AffectationsResolues(clA, &sc, "2026-06-01")
	if len(sousSc) != 0 {
		t.Fatalf("sous le scénario, le serveur doit avoir disparu de clA : %+v", sousSc)
	}

	// retirer une seconde fois avec une date plus tardive : la même ligne du
	// scénario est simplement refermée plus tard, pas de nouvelle ligne créée.
	if err := d.RetirerDuScenario(srv, sc, "2026-03-01"); err != nil {
		t.Fatalf("second retrait (même ligne) : %v", err)
	}
	vies, _ := d.ListerAffectationsServeur(srv)
	if len(vies) != 2 { // réel + une seule ligne de scénario, ré-éditée
		t.Fatalf("attendu 2 lignes au total (pas de doublon) : %+v", vies)
	}

	// date de fin antérieure au début de la ligne retirée : refusé.
	if err := d.RetirerDuScenario(srv, sc, "2019-01-01"); !errors.Is(err, ErrValidation) {
		t.Fatalf("fin antérieure au début : attendu ErrValidation, obtenu %v", err)
	}
}

// TestAffectationsResolues couvre les trois formes de surcharge d'un
// scénario sur les affectations : déplacement d'un serveur réel, ajout pur
// (serveur hypothétique), et retrait sans réaffectation — voir la godoc
// d'AffectationsResolues pour la règle appliquée.
func TestAffectationsResolues(t *testing.T) {
	d, r, deplace := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r)
	sc := scenarioDeTest(t, d.base)
	aDate := "2027-06-01"

	// déplace : réel sur clA, le scénario le fait basculer sur clB.
	if err := d.Affecter(deplace, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(deplace, clB, "2027-01-01", &sc, nil); err != nil {
		t.Fatal(err)
	}

	// intact : réel sur clA, aucune surcharge du scénario, doit y rester.
	intact, err := d.CreerServeur(Serveur{PhysicalName: ptrStr("intact"), Statut: StatutEnService})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(intact.ID, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}

	// retire : réel sur clA, le scénario l'en retire sans le réaffecter.
	retire, err := d.CreerServeur(Serveur{PhysicalName: ptrStr("retire"), Statut: StatutEnService})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(retire.ID, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(retire.ID, clA, "2027-01-01", &sc, nil); err != nil {
		t.Fatal(err)
	}
	retireAffSc, _ := d.ListerAffectationsCluster(clA, &sc, aDate)
	var idAffRetireSc int64
	for _, a := range retireAffSc {
		if a.ServeurID == retire.ID && a.ScenarioID != nil {
			idAffRetireSc = a.ID
		}
	}
	if err := d.Desaffecter(idAffRetireSc, "2027-03-01"); err != nil {
		t.Fatal(err)
	}

	// ajout : serveur hypothétique du scénario, seulement sur clB.
	ajout, err := d.CreerServeur(Serveur{PhysicalName: ptrStr("ajout"), Statut: StatutHypothese, ScenarioID: &sc})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(ajout.ID, clB, "2027-02-01", &sc, nil); err != nil {
		t.Fatal(err)
	}

	// --- sous le réel (scenarioID nil) : rien de tout ceci n'apparaît sur clB,
	// et clA porte encore les trois serveurs réels.
	reelA, err := d.AffectationsResolues(clA, nil, aDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(reelA) != 3 {
		t.Fatalf("réel clA attendu 3 (déplacé+intact+retiré, tous encore réels) : %+v", reelA)
	}

	// --- sous le scénario, sur clA : seul « intact » reste. « déplacé » est
	// parti sur clB, « retiré » n'a plus d'affectation effective.
	scA, err := d.AffectationsResolues(clA, &sc, aDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(scA) != 1 || scA[0].ServeurID != intact.ID {
		t.Fatalf("clA sous scénario attendu seulement « intact » : %+v", scA)
	}

	// --- sous le scénario, sur clB : « déplacé » et « ajout ».
	scB, err := d.AffectationsResolues(clB, &sc, aDate)
	if err != nil {
		t.Fatal(err)
	}
	if len(scB) != 2 {
		t.Fatalf("clB sous scénario attendu 2 (déplacé+ajout) : %+v", scB)
	}
	var vus map[int64]bool = map[int64]bool{}
	for _, a := range scB {
		vus[a.ServeurID] = true
	}
	if !vus[deplace] || !vus[ajout.ID] {
		t.Fatalf("clB sous scénario doit contenir déplacé et ajout : %+v", scB)
	}
}

// mustActive renvoie l'id de l'affectation active du serveur dans le réel.
func mustActive(t *testing.T, d *Depot, serveurID int64) int64 {
	t.Helper()
	list, err := d.ListerAffectationsServeur(serveurID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.DateFin == nil && a.ScenarioID == nil {
			return a.ID
		}
	}
	t.Fatal("aucune affectation active")
	return 0
}
