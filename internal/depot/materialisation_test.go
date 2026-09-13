package depot

import (
	"errors"
	"testing"
)

// TestMaterialiser pose une hypothèse complète (v2.3) : trois serveurs
// hypothétiques répartis sur deux zones, rattachés à la révision du
// modèle, affectés au cluster dans le scénario, et un serveur installé non
// conservé retiré du cluster — le tout visible par AffectationsResolues,
// et le réel intact.
func TestMaterialiser(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	semerCatalogue(t, d.base)
	cl := clusterDeTest(t, d.base, r, "ElasticHot")
	sc := scenarioDeTest(t, d.base)

	exec(t, d.base, `INSERT INTO zone (id, code, libelle) VALUES (11, 'DCA', 'A'), (12, 'DCB', 'B')`)

	ancien, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("ANCIEN"), Statut: StatutEnService})
	if err := d.Affecter(ancien.ID, cl, "2020-01-01", nil, nil); err != nil {
		t.Fatal(err)
	}

	crees, err := d.Materialiser(Materialisation{
		ScenarioID: sc, ClusterID: cl, RevisionID: 1, Nb: 3, DateDebut: "2027-01-01",
		Prefixe: "HYP-ElasticHot-STD", Zones: []int64{11, 12}, Retirer: []int64{ancien.ID},
	})
	if err != nil {
		t.Fatalf("matérialisation : %v", err)
	}
	if len(crees) != 3 {
		t.Fatalf("attendu 3 serveurs créés : %v", crees)
	}

	sousSc, _ := d.AffectationsResolues(cl, &sc, "2027-12-31")
	if len(sousSc) != 3 {
		t.Fatalf("sous le scénario, le cluster doit porter les 3 hypothétiques et plus l'ancien : %+v", sousSc)
	}
	reel, _ := d.AffectationsResolues(cl, nil, "2027-12-31")
	if len(reel) != 1 || reel[0].ServeurID != ancien.ID {
		t.Fatalf("le réel doit rester inchangé : %+v", reel)
	}

	// attributs des hypothétiques : statut, scénario, nom, zone alterné,
	// révision rattachée.
	var noms []string
	var dcs []int64
	for _, id := range crees {
		s, err := d.LireServeur(id)
		if err != nil {
			t.Fatal(err)
		}
		if s.Statut != StatutHypothese || s.ScenarioID == nil || *s.ScenarioID != sc {
			t.Fatalf("serveur %d : statut %s, scénario %v", id, s.Statut, s.ScenarioID)
		}
		noms = append(noms, *s.PhysicalName)
		dcs = append(dcs, *s.ZoneID)
		ratt, _ := d.ListerRattachements(id)
		if len(ratt) != 1 || ratt[0].RevisionID != 1 || ratt[0].DateDebut != "2027-01-01" {
			t.Fatalf("serveur %d : rattachement de révision inattendu : %+v", id, ratt)
		}
	}
	if noms[0] != "HYP-ElasticHot-STD-01" || noms[2] != "HYP-ElasticHot-STD-03" {
		t.Fatalf("noms : %v", noms)
	}
	if dcs[0] != 11 || dcs[1] != 12 || dcs[2] != 11 {
		t.Fatalf("répartition circulaire attendue 11,12,11 : %v", dcs)
	}

	// rien à poser : refusé.
	if _, err := d.Materialiser(Materialisation{
		ScenarioID: sc, ClusterID: cl, RevisionID: 1, Nb: 0, DateDebut: "2027-01-01",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("rien à poser : attendu ErrValidation, obtenu %v", err)
	}

	// tout ou rien : un retrait impossible (serveur sans affectation) annule
	// aussi les créations de la même matérialisation.
	orphelin, _ := d.CreerServeur(Serveur{PhysicalName: ptrStr("ORPHELIN"), Statut: StatutEnService})
	avant, _ := d.ListerServeurs(FiltreServeur{ScenarioID: &sc})
	_, err = d.Materialiser(Materialisation{
		ScenarioID: sc, ClusterID: cl, RevisionID: 1, Nb: 2, DateDebut: "2027-06-01",
		Prefixe: "X", Retirer: []int64{orphelin.ID},
	})
	if !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("retrait impossible : attendu ErrIntrouvable, obtenu %v", err)
	}
	apres, _ := d.ListerServeurs(FiltreServeur{ScenarioID: &sc})
	if len(apres) != len(avant) {
		t.Fatalf("une matérialisation refusée ne doit rien laisser : %d serveurs avant, %d après", len(avant), len(apres))
	}
}
