package depot

import (
	"errors"
	"testing"
)

func TestMetriqueCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	m, err := d.CreerMetrique(Metrique{Code: "DISQUE_UTILE_TO", Libelle: "Disque utile", Unite: "TO"})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if m.ID == 0 {
		t.Fatal("identifiant non attribué")
	}

	relu, err := d.LireMetrique(m.ID)
	if err != nil || relu != m {
		t.Fatalf("relecture : %v / %+v", err, relu)
	}
	parCode, err := d.LireMetriqueParCode("DISQUE_UTILE_TO")
	if err != nil || parCode.ID != m.ID {
		t.Fatalf("lecture par code : %v / %+v", err, parCode)
	}

	m.Libelle = "Disque utile net"
	m.Unite = "TO"
	if err := d.ModifierMetrique(m); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireMetrique(m.ID)
	if relu.Libelle != "Disque utile net" {
		t.Fatalf("modification non persistée : %+v", relu)
	}
}

func TestMetriqueValidationEtUnicite(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerMetrique(Metrique{Code: "", Libelle: "x", Unite: "TO"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("attendu ErrValidation, obtenu %v", err)
	}
	if _, err := d.CreerMetrique(Metrique{Code: "CPU", Libelle: "Coeurs", Unite: "CORE"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerMetrique(Metrique{Code: "CPU", Libelle: "autre", Unite: "CORE"}); !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
	if _, err := d.LireMetrique(999); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("attendu ErrIntrouvable, obtenu %v", err)
	}
}
