package depot

import (
	"errors"
	"testing"
)

func TestTierCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	tr, err := d.CreerTier(Tier{Code: " HOT ", Libelle: "  Hot  ", Ordre: 10})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if tr.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if tr.Code != "HOT" || tr.Libelle != "Hot" {
		t.Fatalf("champs non normalisés : %+v", tr)
	}

	relu, err := d.LireTier(tr.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu != tr {
		t.Fatalf("relecture divergente : %+v vs %+v", relu, tr)
	}

	parCode, err := d.LireTierParCode("HOT")
	if err != nil || parCode.ID != tr.ID {
		t.Fatalf("lecture par code : %v / %+v", err, parCode)
	}

	tr.Libelle = "Hot (SSD)"
	tr.Ordre = 1
	if err := d.ModifierTier(tr); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireTier(tr.ID)
	if relu.Libelle != "Hot (SSD)" || relu.Ordre != 1 {
		t.Fatalf("modification non persistée : %+v", relu)
	}
}

func TestTierTri(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerTier(Tier{Code: "WARM", Libelle: "Warm", Ordre: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerTier(Tier{Code: "COLD", Libelle: "Cold", Ordre: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerTier(Tier{Code: "HOT", Libelle: "Hot", Ordre: 10}); err != nil {
		t.Fatal(err)
	}

	liste, err := d.ListerTiers()
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, tr := range liste {
		codes = append(codes, tr.Code)
	}
	if len(codes) != 3 || codes[0] != "HOT" || codes[1] != "COLD" || codes[2] != "WARM" {
		t.Fatalf("tri ordre puis code non respecté : %v", codes)
	}
}

func TestTierCodeUnique(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerTier(Tier{Code: "X", Libelle: "un"}); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreerTier(Tier{Code: "X", Libelle: "deux"})
	if !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
}

func TestTierValidation(t *testing.T) {
	d := depotDeTest(t)
	for _, cas := range []Tier{
		{Code: "", Libelle: "ok"},
		{Code: "  ", Libelle: "ok"},
		{Code: "OK", Libelle: ""},
	} {
		if _, err := d.CreerTier(cas); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}
}

func TestTierIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireTier(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierTier(Tier{ID: 404, Code: "A", Libelle: "b"}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
}
