package depot

import (
	"errors"
	"testing"
)

func TestScenarioCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	if _, err := d.CreerScenario(Scenario{Nom: "H1", Description: ""}); !errors.Is(err, ErrValidation) {
		t.Fatalf("description obligatoire : attendu ErrValidation, obtenu %v", err)
	}

	s, err := d.CreerScenario(Scenario{Nom: "Hypothèse 2027", Description: "Achat DENSE en 2027"})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if s.Statut != ScenarioBrouillon {
		t.Fatalf("un scénario neuf doit être BROUILLON, obtenu %s", s.Statut)
	}

	if err := d.ActiverScenario(s.ID); err != nil {
		t.Fatalf("activation : %v", err)
	}
	if err := d.AbandonnerScenario(s.ID); err != nil {
		t.Fatalf("abandon : %v", err)
	}
	relu, _ := d.LireScenario(s.ID)
	if relu.Statut != ScenarioAbandonne || relu.DateCloture == nil {
		t.Fatalf("abandon doit clore : %+v", relu)
	}

	// abandon / reprise sans ressaisie
	if err := d.ReprendreScenario(s.ID); err != nil {
		t.Fatalf("reprise : %v", err)
	}
	relu, _ = d.LireScenario(s.ID)
	if relu.Statut != ScenarioActif || relu.DateCloture != nil {
		t.Fatalf("reprise doit rouvrir : %+v", relu)
	}

	// exclusion des clos de la liste courante
	clos, _ := d.CreerScenario(Scenario{Nom: "C", Description: "x"})
	d.AbandonnerScenario(clos.ID)
	courants, _ := d.ListerScenarios(false)
	if len(courants) != 1 || courants[0].ID != s.ID {
		t.Fatalf("liste courante : %+v", courants)
	}
	if tous, _ := d.ListerScenarios(true); len(tous) != 2 {
		t.Fatalf("liste complète : %+v", tous)
	}
}

func TestScenarioPromotion(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	cl := clusterDeTest(t, d.base, r, "ElasticHot")

	s, _ := d.CreerScenario(Scenario{Nom: "Ajout 2027", Description: "3 serveurs"})
	concurrent, _ := d.CreerScenario(Scenario{Nom: "Variante", Description: "autre piste"})
	d.ActiverScenario(s.ID)
	d.ActiverScenario(concurrent.ID)

	// un serveur hypothétique porté par le scénario, affecté dans le calque
	res, err := d.base.Exec(
		`INSERT INTO serveur (physical_name, statut, scenario_id, date_entree)
		 VALUES ('PHYX', 'HYPOTHESE', ?, '2027-01-01')`, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	serveurID, _ := res.LastInsertId()
	exec(t, d.base,
		`INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id)
		 VALUES (?, ?, '2027-01-01', ?)`, serveurID, cl, s.ID)

	if err := d.Promouvoir(s.ID, []int64{concurrent.ID}); err != nil {
		t.Fatalf("promotion : %v", err)
	}

	// le serveur est passé dans le réel, statut COMMANDE
	var statut string
	var scen *int64
	d.base.QueryRow(`SELECT statut, scenario_id FROM serveur WHERE id = ?`, serveurID).
		Scan(&statut, &scen)
	if statut != "COMMANDE" || scen != nil {
		t.Fatalf("serveur promu : statut=%s scenario=%v", statut, scen)
	}
	// l'affectation a perdu son scenario_id
	var affScen *int64
	d.base.QueryRow(`SELECT scenario_id FROM affectation WHERE serveur_id = ?`, serveurID).Scan(&affScen)
	if affScen != nil {
		t.Fatalf("affectation encore dans le calque : %v", affScen)
	}

	sRelu, _ := d.LireScenario(s.ID)
	if sRelu.Statut != ScenarioRetenu {
		t.Fatalf("scénario promu doit être RETENU : %s", sRelu.Statut)
	}
	cRelu, _ := d.LireScenario(concurrent.ID)
	if cRelu.Statut != ScenarioAbandonne {
		t.Fatalf("scénario concurrent doit être ABANDONNE : %s", cRelu.Statut)
	}

	// on ne promeut pas deux fois
	if err := d.Promouvoir(s.ID, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("seconde promotion : attendu ErrValidation, obtenu %v", err)
	}
}

// TestScenarioPromotionSurcharges vérifie que la promotion applique au réel
// la même règle que la lecture (modele-donnees.md §6) pour chaque forme de
// surcharge : un serveur réel déplacé, un serveur réel retiré, une variable
// et une contrainte surchargées à une portée où le réel a déjà une valeur.
// Basculer bêtement scenario_id à NULL ne suffit à aucun de ces cas.
func TestScenarioPromotionSurcharges(t *testing.T) {
	d, r, deplace := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r)
	s, err := d.CreerScenario(Scenario{Nom: "Bascule", Description: "test"})
	if err != nil {
		t.Fatal(err)
	}

	// déplacé : réel clA depuis 2020, scénario clB depuis 2026-03-01.
	if err := d.Affecter(deplace, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.AffecterDansScenario(deplace, clB, "2026-03-01", s.ID, nil); err != nil {
		t.Fatal(err)
	}

	// retiré : réel clA depuis 2020, retrait dans le scénario au 2026-06-30.
	retire, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("retire"), Statut: StatutEnService})
	if err := d.Affecter(retire.ID, clA, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.RetirerDuScenario(retire.ID, s.ID, "2026-06-30"); err != nil {
		t.Fatal(err)
	}

	// variable : réel 5 au niveau global 2027, scénario 10 à la même portée.
	v, err := d.CreerVariable(Variable{Code: "debit", Libelle: "Débit"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.DefinirValeur(v.ID, nil, 2027, PorteeVariable{}, 5, nil, nil); err != nil {
		t.Fatal(err)
	}
	sc := s.ID
	if _, err := d.DefinirValeur(v.ID, &sc, 2027, PorteeVariable{}, 10, nil, nil); err != nil {
		t.Fatal(err)
	}

	// contrainte : réel MIN_TOTAL 3, scénario 6, même cluster/année/type.
	trois, six := 3.0, 6.0
	if _, err := d.DefinirContrainte(clA, nil, 2027, "MIN_TOTAL", &trois, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DefinirContrainte(clA, &sc, 2027, "MIN_TOTAL", &six, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := d.Promouvoir(s.ID, nil); err != nil {
		t.Fatalf("promotion avec surcharges : %v", err)
	}

	// Le réel, après promotion, doit être exactement ce que le scénario montrait.
	surA, _ := d.AffectationsResolues(clA, nil, "2026-09-01")
	if len(surA) != 0 {
		t.Fatalf("clA doit être vide dans le réel après promotion (déplacé + retiré) : %+v", surA)
	}
	surB, _ := d.AffectationsResolues(clB, nil, "2026-09-01")
	if len(surB) != 1 || surB[0].ServeurID != deplace {
		t.Fatalf("clB doit porter le serveur déplacé dans le réel : %+v", surB)
	}
	// et l'historique reste lisible : l'ancienne vie sur clA est close la
	// veille du déplacement, pas effacée.
	vies, _ := d.ListerAffectationsServeur(deplace)
	if len(vies) != 2 || vies[0].DateFin == nil || *vies[0].DateFin != "2026-02-28" || vies[1].ScenarioID != nil {
		t.Fatalf("vies du serveur déplacé après promotion : %+v", vies)
	}
	viesRetire, _ := d.ListerAffectationsServeur(retire.ID)
	if len(viesRetire) != 1 || viesRetire[0].DateFin == nil || *viesRetire[0].DateFin != "2026-06-30" {
		t.Fatalf("le serveur retiré doit avoir une seule vie, close au 2026-06-30 : %+v", viesRetire)
	}

	// plus aucune ligne ne référence le scénario
	for _, table := range []string{"affectation", "variable_valeur", "contrainte"} {
		var n int
		d.base.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE scenario_id = ?`, s.ID).Scan(&n)
		if n != 0 {
			t.Fatalf("%s : %d ligne(s) encore dans le calque", table, n)
		}
	}

	// variable : une seule valeur réelle à cette portée, à 10, avec l'historique 5 -> 10.
	valeurs, _ := d.ListerValeurs(v.ID, nil, nil)
	if len(valeurs) != 1 || valeurs[0].Valeur != 10 {
		t.Fatalf("valeur réelle après promotion : %+v", valeurs)
	}
	histo, _ := d.HistoriqueValeur(valeurs[0].ID)
	if len(histo) != 1 || histo[0].Ancienne != 5 || histo[0].Nouvelle != 10 {
		t.Fatalf("la promotion doit historiser l'écrasement 5 -> 10 : %+v", histo)
	}

	// contrainte : une seule ligne réelle, à 6.
	contraintes, _ := d.ListerContraintes(clA, 2027, nil)
	if len(contraintes) != 1 || contraintes[0].Valeur == nil || *contraintes[0].Valeur != 6 {
		t.Fatalf("contrainte réelle après promotion : %+v", contraintes)
	}
}

// TestScenarioPromotionRefusChevauchement : promouvoir un déplacement daté
// avant le début de l'affectation réelle courante est impossible sans
// réécrire l'histoire — refusé, et rien n'est écrit.
func TestScenarioPromotionRefusChevauchement(t *testing.T) {
	d, r, srv := serveurAffectDeTest(t)
	clA, clB := deuxClusters(t, d, r)
	s, _ := d.CreerScenario(Scenario{Nom: "Anachronique", Description: "test"})

	if err := d.Affecter(srv, clA, "2025-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.AffecterDansScenario(srv, clB, "2024-06-01", s.ID, nil); err != nil {
		t.Fatal(err)
	}
	err := d.Promouvoir(s.ID, nil)
	if !errors.Is(err, ErrChevauchement) {
		t.Fatalf("attendu ErrChevauchement, obtenu %v", err)
	}
	relu, _ := d.LireScenario(s.ID)
	if relu.Statut != ScenarioBrouillon {
		t.Fatalf("la promotion refusée ne doit rien changer : statut %s", relu.Statut)
	}
	vies, _ := d.ListerAffectationsServeur(srv)
	if len(vies) != 2 {
		t.Fatalf("rien ne doit avoir bougé : %+v", vies)
	}
	for _, v := range vies {
		if v.DateFin != nil {
			t.Fatalf("aucune ligne ne doit avoir été close par une promotion refusée : %+v", vies)
		}
		if v.ClusterID == clB && v.ScenarioID == nil {
			t.Fatalf("la ligne du scénario ne doit pas être passée dans le réel : %+v", vies)
		}
	}
}
