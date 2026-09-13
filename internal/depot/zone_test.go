package depot

import (
	"errors"
	"testing"
)

// ptrChaine facilite la construction des champs *string dans les tests.
func ptrChaine(s string) *string { return &s }

func TestZoneCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	dc, err := d.CreerZone(Zone{Code: " DC1 ", Libelle: "  Zone 1  ", Site: ptrChaine(" Paris ")})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if dc.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if dc.Code != "DC1" || dc.Libelle != "Zone 1" {
		t.Fatalf("champs non normalisés : %+v", dc)
	}
	if dc.Site == nil || *dc.Site != "Paris" {
		t.Fatalf("site non normalisé : %+v", dc.Site)
	}

	relu, err := d.LireZone(dc.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu.ID != dc.ID || relu.Code != dc.Code || relu.Libelle != dc.Libelle {
		t.Fatalf("relecture divergente : %+v vs %+v", relu, dc)
	}
	if relu.Site == nil || *relu.Site != "Paris" {
		t.Fatalf("site non persisté : %+v", relu.Site)
	}

	parCode, err := d.LireZoneParCode("DC1")
	if err != nil || parCode.ID != dc.ID {
		t.Fatalf("lecture par code : %v / %+v", err, parCode)
	}

	// Modification : libellé changé et site effacé (chaîne blanche → NULL).
	dc.Libelle = "Zone Nord"
	dc.Site = ptrChaine("   ")
	if err := d.ModifierZone(dc); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireZone(dc.ID)
	if relu.Libelle != "Zone Nord" {
		t.Fatalf("modification non persistée : %+v", relu)
	}
	if relu.Site != nil {
		t.Fatalf("site attendu NULL après effacement, obtenu %q", *relu.Site)
	}
}

func TestZoneSiteAbsent(t *testing.T) {
	d := depotDeTest(t)
	dc, err := d.CreerZone(Zone{Code: "DC2", Libelle: "Zone 2"})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if dc.Site != nil {
		t.Fatalf("site attendu nil, obtenu %q", *dc.Site)
	}
	relu, _ := d.LireZone(dc.ID)
	if relu.Site != nil {
		t.Fatalf("site attendu NULL en base, obtenu %q", *relu.Site)
	}
}

func TestZoneCodeUnique(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerZone(Zone{Code: "X", Libelle: "un"}); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreerZone(Zone{Code: "X", Libelle: "deux"})
	if !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
}

func TestZoneValidation(t *testing.T) {
	d := depotDeTest(t)
	for _, cas := range []Zone{
		{Code: "", Libelle: "ok"},
		{Code: "  ", Libelle: "ok"},
		{Code: "OK", Libelle: ""},
	} {
		if _, err := d.CreerZone(cas); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}
}

func TestZoneIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireZone(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierZone(Zone{ID: 404, Code: "A", Libelle: "b"}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
}
