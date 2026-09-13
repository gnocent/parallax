package depot

import (
	"errors"
	"testing"
)

func TestProjetCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	p, err := d.CreerProjet(Projet{Code: " LOGS ", Libelle: "  Log Management  "})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if p.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if p.Code != "LOGS" || p.Libelle != "Log Management" {
		t.Fatalf("champs non normalisés : %+v", p)
	}
	if !p.Actif {
		t.Fatal("un projet neuf doit être actif")
	}

	relu, err := d.LireProjet(p.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu != p {
		t.Fatalf("relecture divergente : %+v vs %+v", relu, p)
	}

	parCode, err := d.LireProjetParCode("LOGS")
	if err != nil || parCode.ID != p.ID {
		t.Fatalf("lecture par code : %v / %+v", err, parCode)
	}

	p.Libelle = "Log Management Production"
	if err := d.ModifierProjet(p); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireProjet(p.ID)
	if relu.Libelle != "Log Management Production" {
		t.Fatalf("modification non persistée : %+v", relu)
	}
}

func TestProjetCodeUnique(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerProjet(Projet{Code: "X", Libelle: "un"}); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreerProjet(Projet{Code: "X", Libelle: "deux"})
	if !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
}

func TestProjetValidation(t *testing.T) {
	d := depotDeTest(t)
	for _, cas := range []Projet{
		{Code: "", Libelle: "ok"},
		{Code: "  ", Libelle: "ok"},
		{Code: "OK", Libelle: ""},
	} {
		if _, err := d.CreerProjet(cas); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}
}

func TestProjetIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireProjet(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierProjet(Projet{ID: 404, Code: "A", Libelle: "b"}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ArchiverProjet(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("archivage : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestProjetArchivage(t *testing.T) {
	d := depotDeTest(t)
	a, _ := d.CreerProjet(Projet{Code: "A", Libelle: "a"})
	b, _ := d.CreerProjet(Projet{Code: "B", Libelle: "b"})

	if err := d.ArchiverProjet(a.ID); err != nil {
		t.Fatalf("archivage : %v", err)
	}

	actifs, err := d.ListerProjets(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(actifs) != 1 || actifs[0].ID != b.ID {
		t.Fatalf("l'archivé ne doit plus figurer dans la liste courante : %+v", actifs)
	}

	tous, _ := d.ListerProjets(true)
	if len(tous) != 2 {
		t.Fatalf("l'archivé reste lisible avec inclureInactifs : %+v", tous)
	}

	if err := d.ReactiverProjet(a.ID); err != nil {
		t.Fatalf("réactivation : %v", err)
	}
	if relu, _ := d.LireProjet(a.ID); !relu.Actif {
		t.Fatal("réactivation non persistée")
	}
}
