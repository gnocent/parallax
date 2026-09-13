package depot

import (
	"errors"
	"testing"
)

// semerCatalogue insère un modèle en lease et deux révisions, par SQL direct.
// Renvoie (modeleID, revision1, revision2).
func semerCatalogue(t *testing.T, c conn) (int64, int64, int64) {
	t.Helper()
	exec(t, c, `INSERT INTO modele
		(id, type, annee, code, mode_financement, duree_lease_mois, date_debut_lease)
		VALUES (1, 'STD', 2020, 'STD-2020', 'LEASE', 60, '2020-06-01')`)
	exec(t, c, `INSERT INTO revision (id, modele_id, numero, date_effet) VALUES
		(1, 1, 1, '2020-06-01'),
		(2, 1, 2, '2024-03-01')`)
	return 1, 1, 2
}

// scenarioDeTest insère un scénario minimal et renvoie son id.
func scenarioDeTest(t *testing.T, c conn) int64 {
	t.Helper()
	res, err := c.Exec(
		`INSERT INTO scenario (nom, description, statut, date_creation)
		 VALUES ('Hypothèse 2027', 'test', 'ACTIF', '2026-09-01')`)
	if err != nil {
		t.Fatalf("fixture scénario : %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestServeurCycleDeVie(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)

	s, err := d.CreerServeur(Serveur{
		PhysicalName: ptrStr("PHY001"),
		Hostname:     ptrStr("esh01"),
		ZoneID:       ptrI64(r.DC1),
		Statut:       StatutEnService,
		DateEntree:   ptrStr("2020-07-01"),
	})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if s.ID == 0 {
		t.Fatal("identifiant non attribué")
	}

	s.Hostname = ptrStr("esh01-new")
	s.CodeAppli = ptrStr("LOGS")
	s.Statut = StatutDecommissionne // ignoré par ModifierServeur
	if err := d.ModifierServeur(s); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ := d.LireServeur(s.ID)
	if relu.Hostname == nil || *relu.Hostname != "esh01-new" {
		t.Fatalf("attribut non persisté : %+v", relu)
	}
	if relu.Statut != StatutEnService {
		t.Fatalf("ModifierServeur ne doit pas toucher le statut : %q", relu.Statut)
	}

	if err := d.ChangerStatut(s.ID, StatutDecommissionne); err != nil {
		t.Fatalf("changement de statut : %v", err)
	}
	if relu, _ := d.LireServeur(s.ID); relu.Statut != StatutDecommissionne {
		t.Fatalf("statut non persisté : %q", relu.Statut)
	}
}

func TestServeurStatutInvalide(t *testing.T) {
	d := depotDeTest(t)
	semerRefs(t, d.base)

	if _, err := d.CreerServeur(Serveur{Statut: "N_IMPORTE_QUOI"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("création : attendu ErrValidation, obtenu %v", err)
	}
	s, _ := d.CreerServeur(Serveur{Statut: StatutEnService})
	if err := d.ChangerStatut(s.ID, "BIDON"); !errors.Is(err, ErrValidation) {
		t.Fatalf("changement : attendu ErrValidation, obtenu %v", err)
	}
}

func TestServeurHypotheseInvariant5(t *testing.T) {
	d := depotDeTest(t)
	semerRefs(t, d.base)
	sc := scenarioDeTest(t, d.base)

	// création HYPOTHESE sans scénario : refusée en code
	if _, err := d.CreerServeur(Serveur{Statut: StatutHypothese}); !errors.Is(err, ErrValidation) {
		t.Fatalf("création HYPOTHESE sans scénario : attendu ErrValidation, obtenu %v", err)
	}

	// avec scénario : acceptée
	h, err := d.CreerServeur(Serveur{Statut: StatutHypothese, ScenarioID: &sc})
	if err != nil {
		t.Fatalf("création HYPOTHESE avec scénario : %v", err)
	}

	// ôter le scénario d'un serveur HYPOTHESE : refusé par le CHECK (traduit)
	if err := d.DefinirScenario(h.ID, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("retrait du scénario : attendu ErrValidation, obtenu %v", err)
	}

	// passer un serveur réel à HYPOTHESE sans scénario : refusé par le CHECK
	reel, _ := d.CreerServeur(Serveur{Statut: StatutEnService})
	if err := d.ChangerStatut(reel.ID, StatutHypothese); !errors.Is(err, ErrValidation) {
		t.Fatalf("passage à HYPOTHESE sans scénario : attendu ErrValidation, obtenu %v", err)
	}
}

func TestServeurDefinirScenario(t *testing.T) {
	d := depotDeTest(t)
	semerRefs(t, d.base)
	sc := scenarioDeTest(t, d.base)

	s, _ := d.CreerServeur(Serveur{Statut: StatutCommande})
	if err := d.DefinirScenario(s.ID, &sc); err != nil {
		t.Fatalf("rattachement : %v", err)
	}
	if relu, _ := d.LireServeur(s.ID); relu.ScenarioID == nil || *relu.ScenarioID != sc {
		t.Fatalf("scénario non persisté : %+v", relu)
	}
	if err := d.DefinirScenario(s.ID, nil); err != nil {
		t.Fatalf("promotion (retour au réel) : %v", err)
	}
	if relu, _ := d.LireServeur(s.ID); relu.ScenarioID != nil {
		t.Fatalf("promotion non persistée : %+v", relu)
	}

	// scénario inexistant
	if err := d.DefinirScenario(s.ID, ptrI64(999)); !errors.Is(err, ErrReference) {
		t.Fatalf("scénario inexistant : attendu ErrReference, obtenu %v", err)
	}
}

func TestServeurListerFiltre(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	sc := scenarioDeTest(t, d.base)

	s1, _ := d.CreerServeur(Serveur{Statut: StatutEnService, ZoneID: ptrI64(r.DC1)})
	d.CreerServeur(Serveur{Statut: StatutDecommissionne, ZoneID: ptrI64(r.DC2)})
	d.CreerServeur(Serveur{Statut: StatutHypothese, ScenarioID: &sc, ZoneID: ptrI64(r.DC1)})

	// réel seulement quand ScenarioID est nil
	reels, err := d.ListerServeurs(FiltreServeur{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reels) != 2 {
		t.Fatalf("réel seulement attendu, obtenu %d", len(reels))
	}

	// réel + scénario
	avecScenario, _ := d.ListerServeurs(FiltreServeur{ScenarioID: &sc})
	if len(avecScenario) != 3 {
		t.Fatalf("réel + scénario attendu 3, obtenu %d", len(avecScenario))
	}

	// filtre statut
	enService, _ := d.ListerServeurs(FiltreServeur{Statut: ptrStr(StatutEnService)})
	if len(enService) != 1 || enService[0].ID != s1.ID {
		t.Fatalf("filtre statut : %+v", enService)
	}

	// filtre zone
	dc1, _ := d.ListerServeurs(FiltreServeur{ZoneID: ptrI64(r.DC1)})
	if len(dc1) != 1 {
		t.Fatalf("filtre zone (réel) : %d", len(dc1))
	}
}

func TestServeurSansAffectationActive(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	semerCatalogue(t, d.base)
	cl := clusterDeTest(t, d.base, r, "ElasticHot")
	sc := scenarioDeTest(t, d.base)

	// A : réel, révision ouverte (lease 2020-06-01 + 60 mois = 2025-06-01), sans affectation
	a, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("A"), Statut: StatutEnService})
	exec(t, d.base, `INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, 1, '2020-07-01')`, a.ID)

	// B : réel, révision ouverte, AVEC affectation active -> exclu
	b, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("B"), Statut: StatutEnService})
	exec(t, d.base, `INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, 1, '2020-07-01')`, b.ID)
	exec(t, d.base, `INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (?, ?, '2020-07-01')`, b.ID, cl)

	// modèle acheté (sans lease) + sa révision
	exec(t, d.base, `INSERT INTO modele (id, type, annee, code, mode_financement) VALUES (2, 'ECO', 2023, 'ECO-2023', 'ACHAT')`)
	exec(t, d.base, `INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (3, 2, 1, '2023-01-01')`)

	// D : réel, révision ouverte sur un modèle acheté -> FinLease nil, affectation CLOSE -> inclus
	dd, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("D"), Statut: StatutEnService})
	exec(t, d.base, `INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, 3, '2023-02-01')`, dd.ID)
	exec(t, d.base, `INSERT INTO affectation (serveur_id, cluster_id, date_debut, date_fin) VALUES (?, ?, '2023-02-01', '2024-06-30')`, dd.ID, cl)

	// C : réel, aucune révision -> FinLease nil, donc en dernier
	c, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("C"), Statut: StatutEnService})

	// E : hypothèse du scénario, sans affectation -> visible seulement dans le seau du scénario
	e, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("E"), Statut: StatutHypothese, ScenarioID: &sc})
	exec(t, d.base, `INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, 2, '2027-01-01')`, e.ID)

	reel, err := d.ListerServeursSansAffectationActive(nil)
	if err != nil {
		t.Fatalf("réel : %v", err)
	}
	gotIDs := make([]int64, len(reel))
	for i, x := range reel {
		gotIDs[i] = x.Serveur.ID
	}
	// attendu : A (2025-06-01) en tête, puis D et C (sans lease) triés par id
	if len(reel) != 3 || gotIDs[0] != a.ID || gotIDs[1] != dd.ID || gotIDs[2] != c.ID {
		t.Fatalf("liste réelle inattendue : %+v", gotIDs)
	}
	if reel[0].FinLease == nil || *reel[0].FinLease != "2025-06-01" {
		t.Fatalf("fin de lease de A : %v", reel[0].FinLease)
	}
	if reel[1].FinLease != nil || reel[2].FinLease != nil {
		t.Fatalf("les serveurs sans lease doivent finir la liste : %+v", reel)
	}
	for _, x := range reel {
		if x.Serveur.ID == b.ID {
			t.Fatal("B a une affectation active, il ne doit pas apparaître")
		}
		if x.Serveur.ID == e.ID {
			t.Fatal("E est hors du seau réel")
		}
	}

	// F : réel, affecté, puis RETIRÉ dans le scénario -> candidat sous le
	// scénario seulement. G : réel, affecté, DÉPLACÉ dans le scénario vers un
	// autre cluster -> toujours affecté sous le scénario.
	f, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("F"), Statut: StatutEnService})
	if err := d.Affecter(f.ID, cl, "2021-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.RetirerDuScenario(f.ID, sc, "2026-01-01"); err != nil {
		t.Fatal(err)
	}
	g, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("G"), Statut: StatutEnService})
	if err := d.Affecter(g.ID, cl, "2021-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.AffecterDansScenario(g.ID, cl, "2026-01-01", sc, nil); err != nil {
		t.Fatal(err)
	}

	// sous le scénario : E (hypothétique), F (retiré) apparaissent ; G non.
	avecSc, _ := d.ListerServeursSansAffectationActive(&sc)
	vus := map[int64]bool{}
	for _, x := range avecSc {
		vus[x.Serveur.ID] = true
	}
	if !vus[e.ID] || !vus[f.ID] || vus[g.ID] || vus[b.ID] {
		t.Fatalf("sous le scénario attendu E et F, ni G ni B : %+v", avecSc)
	}
	// dans le réel, F est toujours affecté : il ne doit pas y apparaître.
	reelApres, _ := d.ListerServeursSansAffectationActive(nil)
	for _, x := range reelApres {
		if x.Serveur.ID == f.ID {
			t.Fatal("F reste affecté dans le réel, le retrait du scénario ne doit pas le rendre candidat au réel")
		}
	}
}
