package depot

import (
	"errors"
	"testing"

	"parallax/internal/capacity"
)

func TestContrainteDefinitionEtResolution(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	cl := clusterDeTest(t, d.base, r, "ElasticHot")
	s, _ := d.CreerScenario(Scenario{Nom: "3 zones", Description: "passage en stretched 3 zones"})

	// réel : minimum 6, 2 zones
	if _, err := d.DefinirContrainte(cl, nil, 2026, capacity.ContrainteMinTotal, f64(6), nil, nil); err != nil {
		t.Fatalf("min total : %v", err)
	}
	if _, err := d.DefinirContrainte(cl, nil, 2026, capacity.ContrainteNbZones, f64(2), nil, nil); err != nil {
		t.Fatalf("nb dc : %v", err)
	}
	// le scénario surcharge le nombre de zones à 3
	if _, err := d.DefinirContrainte(cl, i64(s.ID), 2026, capacity.ContrainteNbZones, f64(3), nil, nil); err != nil {
		t.Fatalf("surcharge scénario : %v", err)
	}

	// résolution réelle : nb_zone = 2
	reel, err := d.ContraintesResolues(cl, 2026, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reel[capacity.ContrainteNbZones] != 2 || reel[capacity.ContrainteMinTotal] != 6 {
		t.Fatalf("contraintes réelles : %+v", reel)
	}

	// résolution sous scénario : nb_zone surchargé à 3, min_total hérité du réel
	scen, _ := d.ContraintesResolues(cl, 2026, i64(s.ID))
	if scen[capacity.ContrainteNbZones] != 3 || scen[capacity.ContrainteMinTotal] != 6 {
		t.Fatalf("contraintes sous scénario : %+v", scen)
	}

	// upsert : redéfinir le même (cluster, année, type, réel) remplace
	if _, err := d.DefinirContrainte(cl, nil, 2026, capacity.ContrainteMinTotal, f64(8), nil, nil); err != nil {
		t.Fatal(err)
	}
	reel, _ = d.ContraintesResolues(cl, 2026, nil)
	if reel[capacity.ContrainteMinTotal] != 8 {
		t.Fatalf("upsert non appliqué : %+v", reel)
	}
}

func TestContrainteValidation(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	cl := clusterDeTest(t, d.base, r, "K")

	if _, err := d.DefinirContrainte(cl, nil, 2026, "N_IMPORTE_QUOI", f64(1), nil, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("type inconnu : attendu ErrValidation, obtenu %v", err)
	}
	if _, err := d.DefinirContrainte(cl, nil, 2026, capacity.ContrainteEquilibrageZone, nil, nil, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("équilibrage sans portée : attendu ErrValidation, obtenu %v", err)
	}
	p := PorteeContrainteTous
	if _, err := d.DefinirContrainte(cl, nil, 2026, capacity.ContrainteEquilibrageZone, nil, &p, nil); err != nil {
		t.Fatalf("équilibrage avec portée TOUS : %v", err)
	}
}
