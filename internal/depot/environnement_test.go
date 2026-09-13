package depot

import (
	"errors"
	"testing"
)

func TestEnvironnementCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	e, err := d.CreerEnvironnement(Environnement{Code: " PROD ", Libelle: "  Production  ", Ordre: 10})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if e.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if e.Code != "PROD" || e.Libelle != "Production" {
		t.Fatalf("champs non normalisés : %+v", e)
	}

	relu, err := d.LireEnvironnement(e.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu != e {
		t.Fatalf("relecture divergente : %+v vs %+v", relu, e)
	}

	parCode, err := d.LireEnvironnementParCode("PROD")
	if err != nil || parCode.ID != e.ID {
		t.Fatalf("lecture par code : %v / %+v", err, parCode)
	}

	e.Libelle = "Production"
	e.Ordre = 5
	if err := d.ModifierEnvironnement(e); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireEnvironnement(e.ID)
	if relu.Libelle != "Production" || relu.Ordre != 5 {
		t.Fatalf("modification non persistée : %+v", relu)
	}
}

func TestEnvironnementTri(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerEnvironnement(Environnement{Code: "QUAL", Libelle: "Qualification", Ordre: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerEnvironnement(Environnement{Code: "PROD", Libelle: "Production", Ordre: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerEnvironnement(Environnement{Code: "DEV", Libelle: "Développement", Ordre: 5}); err != nil {
		t.Fatal(err)
	}

	liste, err := d.ListerEnvironnements()
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, e := range liste {
		codes = append(codes, e.Code)
	}
	// DEV (ordre 5), puis PROD et QUAL (ordre 20) départagés par code.
	if len(codes) != 3 || codes[0] != "DEV" || codes[1] != "PROD" || codes[2] != "QUAL" {
		t.Fatalf("tri ordre puis code non respecté : %v", codes)
	}
}

func TestEnvironnementCodeUnique(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerEnvironnement(Environnement{Code: "X", Libelle: "un"}); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreerEnvironnement(Environnement{Code: "X", Libelle: "deux"})
	if !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
}

func TestEnvironnementValidation(t *testing.T) {
	d := depotDeTest(t)
	for _, cas := range []Environnement{
		{Code: "", Libelle: "ok"},
		{Code: "  ", Libelle: "ok"},
		{Code: "OK", Libelle: ""},
	} {
		if _, err := d.CreerEnvironnement(cas); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}
}

func TestEnvironnementIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireEnvironnement(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierEnvironnement(Environnement{ID: 404, Code: "A", Libelle: "b"}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
}
