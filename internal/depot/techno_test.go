package depot

import (
	"errors"
	"testing"
)

func TestTechnoCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	tc, err := d.CreerTechno(Techno{Code: " ELASTIC ", Libelle: "  Elasticsearch  "})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if tc.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if tc.Code != "ELASTIC" || tc.Libelle != "Elasticsearch" {
		t.Fatalf("champs non normalisés : %+v", tc)
	}
	if !tc.Actif {
		t.Fatal("une techno neuve doit être active")
	}

	relu, err := d.LireTechno(tc.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu != tc {
		t.Fatalf("relecture divergente : %+v vs %+v", relu, tc)
	}

	parCode, err := d.LireTechnoParCode("ELASTIC")
	if err != nil || parCode.ID != tc.ID {
		t.Fatalf("lecture par code : %v / %+v", err, parCode)
	}

	tc.Libelle = "Elasticsearch 8"
	if err := d.ModifierTechno(tc); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireTechno(tc.ID)
	if relu.Libelle != "Elasticsearch 8" {
		t.Fatalf("modification non persistée : %+v", relu)
	}
}

func TestTechnoCodeUnique(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerTechno(Techno{Code: "X", Libelle: "un"}); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreerTechno(Techno{Code: "X", Libelle: "deux"})
	if !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
}

func TestTechnoValidation(t *testing.T) {
	d := depotDeTest(t)
	for _, cas := range []Techno{
		{Code: "", Libelle: "ok"},
		{Code: "  ", Libelle: "ok"},
		{Code: "OK", Libelle: ""},
	} {
		if _, err := d.CreerTechno(cas); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}
}

func TestTechnoIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireTechno(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierTechno(Techno{ID: 404, Code: "A", Libelle: "b"}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ArchiverTechno(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("archivage : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestTechnoArchivage(t *testing.T) {
	d := depotDeTest(t)
	a, _ := d.CreerTechno(Techno{Code: "A", Libelle: "a"})
	b, _ := d.CreerTechno(Techno{Code: "B", Libelle: "b"})

	if err := d.ArchiverTechno(a.ID); err != nil {
		t.Fatalf("archivage : %v", err)
	}

	actives, err := d.ListerTechnos(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(actives) != 1 || actives[0].ID != b.ID {
		t.Fatalf("l'archivée ne doit plus figurer dans la liste courante : %+v", actives)
	}

	toutes, _ := d.ListerTechnos(true)
	if len(toutes) != 2 {
		t.Fatalf("l'archivée reste lisible avec inclureInactifs : %+v", toutes)
	}

	if err := d.ReactiverTechno(a.ID); err != nil {
		t.Fatalf("réactivation : %v", err)
	}
	if relu, _ := d.LireTechno(a.ID); !relu.Actif {
		t.Fatal("réactivation non persistée")
	}
}
