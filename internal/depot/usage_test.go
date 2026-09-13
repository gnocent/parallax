package depot

import (
	"errors"
	"testing"
)

func TestUsageCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	u, err := d.CreerUsage(UsageFonctionnel{Code: " LOGMGMT ", Libelle: "  Log Management  "})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if u.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if u.Code != "LOGMGMT" || u.Libelle != "Log Management" {
		t.Fatalf("champs non normalisés : %+v", u)
	}

	relu, err := d.LireUsage(u.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu != u {
		t.Fatalf("relecture divergente : %+v vs %+v", relu, u)
	}

	parCode, err := d.LireUsageParCode("LOGMGMT")
	if err != nil || parCode.ID != u.ID {
		t.Fatalf("lecture par code : %v / %+v", err, parCode)
	}

	u.Libelle = "Gestion des logs"
	if err := d.ModifierUsage(u); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireUsage(u.ID)
	if relu.Libelle != "Gestion des logs" {
		t.Fatalf("modification non persistée : %+v", relu)
	}
}

func TestUsageListe(t *testing.T) {
	d := depotDeTest(t)
	for _, code := range []string{"METRO", "APM", "LOGMGMT"} {
		if _, err := d.CreerUsage(UsageFonctionnel{Code: code, Libelle: code}); err != nil {
			t.Fatal(err)
		}
	}
	liste, err := d.ListerUsages()
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, u := range liste {
		codes = append(codes, u.Code)
	}
	if len(codes) != 3 || codes[0] != "APM" || codes[1] != "LOGMGMT" || codes[2] != "METRO" {
		t.Fatalf("tri par code non respecté : %v", codes)
	}
}

func TestUsageCodeUnique(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerUsage(UsageFonctionnel{Code: "X", Libelle: "un"}); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreerUsage(UsageFonctionnel{Code: "X", Libelle: "deux"})
	if !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
}

func TestUsageValidation(t *testing.T) {
	d := depotDeTest(t)
	for _, cas := range []UsageFonctionnel{
		{Code: "", Libelle: "ok"},
		{Code: "  ", Libelle: "ok"},
		{Code: "OK", Libelle: ""},
	} {
		if _, err := d.CreerUsage(cas); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}
}

func TestUsageIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireUsage(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierUsage(UsageFonctionnel{ID: 404, Code: "A", Libelle: "b"}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
}
