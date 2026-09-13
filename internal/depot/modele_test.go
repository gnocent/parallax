package depot

import (
	"errors"
	"testing"
)

// ptrDe fabrique un pointeur sur une valeur littérale, pour renseigner les
// champs nullables dans les tests.
func ptrDe[T any](v T) *T { return &v }

// modeleDeTest insère un modèle minimal et le renvoie.
func modeleDeTest(t *testing.T, d *Depot) Modele {
	t.Helper()
	m, err := d.CreerModele(Modele{Type: "DENSE", Annee: 2025, Code: "DENSE-2025"})
	if err != nil {
		t.Fatalf("fixture modèle : %v", err)
	}
	return m
}

// revisionDeTest insère une révision minimale sur un modèle et la renvoie.
func revisionDeTest(t *testing.T, d *Depot, modeleID int64) Revision {
	t.Helper()
	r, err := d.CreerRevision(Revision{ModeleID: modeleID, DateEffet: "2025-01-15"})
	if err != nil {
		t.Fatalf("fixture révision : %v", err)
	}
	return r
}

// referencerRevision rattache un serveur à une révision par SQL direct, ce qui
// la rend « référencée » et donc immuable hors correction (invariant 1).
func referencerRevision(t *testing.T, d *Depot, revisionID int64) {
	t.Helper()
	res, err := d.base.Exec(`INSERT INTO serveur (statut) VALUES ('EN_SERVICE')`)
	if err != nil {
		t.Fatalf("fixture serveur : %v", err)
	}
	sid, _ := res.LastInsertId()
	exec(t, d.base, `INSERT INTO serveur_revision (serveur_id, revision_id, date_debut)
		VALUES (?, ?, '2025-02-01')`, sid, revisionID)
}

func TestModeleCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	m, err := d.CreerModele(Modele{
		Type:              "  DENSE  ",
		Annee:             2025,
		Code:              "  DENSE-2025  ",
		Description:       ptrDe("Stockage dense"),
		ModeFinancement:   ptrDe("LEASE"),
		DureeLeaseMois:    ptrDe[int64](60),
		DateDebutLease:    ptrDe("2025-04-01"),
		PrixFournisseurHT: ptrDe(42000.0),
		CoutAnnuelHT:      ptrDe(9000.0),
		DureeCoutAnnees:   ptrDe[int64](5),
	})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if m.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if m.Type != "DENSE" || m.Code != "DENSE-2025" {
		t.Fatalf("champs non normalisés : %+v", m)
	}
	if !m.Actif {
		t.Fatal("un modèle neuf doit être actif")
	}

	relu, err := d.LireModele(m.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu.Code != "DENSE-2025" || relu.ModeFinancement == nil || *relu.ModeFinancement != "LEASE" {
		t.Fatalf("relecture divergente : %+v", relu)
	}
	if relu.PrixFournisseurHT == nil || *relu.PrixFournisseurHT != 42000.0 {
		t.Fatalf("prix non persisté : %+v", relu)
	}

	parCode, err := d.LireModeleParCode("DENSE-2025")
	if err != nil || parCode.ID != m.ID {
		t.Fatalf("lecture par code : %v / %+v", err, parCode)
	}

	m.Description = ptrDe("Stockage dense — révisé")
	m.CoutAnnuelHT = ptrDe(9500.0)
	if err := d.ModifierModele(m); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireModele(m.ID)
	if relu.Description == nil || *relu.Description != "Stockage dense — révisé" {
		t.Fatalf("modification non persistée : %+v", relu)
	}

	if err := d.ArchiverModele(m.ID); err != nil {
		t.Fatalf("archivage : %v", err)
	}
	if actifs, _ := d.ListerModeles(false); len(actifs) != 0 {
		t.Fatalf("l'archivé ne doit plus figurer dans la liste courante : %+v", actifs)
	}
	if tous, _ := d.ListerModeles(true); len(tous) != 1 {
		t.Fatalf("l'archivé reste lisible avec inclureInactifs : %+v", tous)
	}
	if err := d.ReactiverModele(m.ID); err != nil {
		t.Fatalf("réactivation : %v", err)
	}
	if relu, _ := d.LireModele(m.ID); !relu.Actif {
		t.Fatal("réactivation non persistée")
	}
}

func TestModeleValidation(t *testing.T) {
	d := depotDeTest(t)
	for _, cas := range []Modele{
		{Type: "", Annee: 2025, Code: "A-2025"},
		{Type: "A", Annee: 2025, Code: "  "},
		{Type: "A", Annee: 1999, Code: "A-1999"},
		{Type: "A", Annee: 0, Code: "A-0"},
		{Type: "A", Annee: 2025, Code: "A-2025", DateDebutLease: ptrDe("01/04/2025")},
	} {
		if _, err := d.CreerModele(cas); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}
}

func TestModeleModeFinancementInvalide(t *testing.T) {
	d := depotDeTest(t)
	_, err := d.CreerModele(Modele{
		Type: "A", Annee: 2025, Code: "A-2025",
		ModeFinancement: ptrDe("LOCATION"),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("mode de financement hors énumération : attendu ErrValidation, obtenu %v", err)
	}
}

func TestModeleConflits(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerModele(Modele{Type: "DENSE", Annee: 2025, Code: "DENSE-2025"}); err != nil {
		t.Fatal(err)
	}

	// même code
	if _, err := d.CreerModele(Modele{Type: "AUTRE", Annee: 2024, Code: "DENSE-2025"}); !errors.Is(err, ErrConflit) {
		t.Fatalf("code dupliqué : attendu ErrConflit, obtenu %v", err)
	}
	// même couple (type, année)
	if _, err := d.CreerModele(Modele{Type: "DENSE", Annee: 2025, Code: "AUTRE-CODE"}); !errors.Is(err, ErrConflit) {
		t.Fatalf("couple (type, année) dupliqué : attendu ErrConflit, obtenu %v", err)
	}
}

func TestModeleIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireModele(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if _, err := d.LireModeleParCode("INEXISTANT"); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture par code : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierModele(Modele{ID: 404, Type: "A", Annee: 2025, Code: "A-2025"}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestFinLease(t *testing.T) {
	cas := []struct {
		debut   string
		mois    int64
		attendu string
	}{
		{"2025-04-01", 60, "2030-04-01"},
		{"2025-04-01", 36, "2028-04-01"},
		{"2024-01-15", 12, "2025-01-15"},
		{"2025-11-01", 6, "2026-05-01"},
	}
	for _, c := range cas {
		got, err := FinLease(c.debut, c.mois)
		if err != nil {
			t.Fatalf("FinLease(%q, %d) : %v", c.debut, c.mois, err)
		}
		if got != c.attendu {
			t.Fatalf("FinLease(%q, %d) = %q, attendu %q", c.debut, c.mois, got, c.attendu)
		}
	}

	if _, err := FinLease("pas une date", 12); !errors.Is(err, ErrValidation) {
		t.Fatalf("date invalide : attendu ErrValidation, obtenu %v", err)
	}
	if _, err := FinLease("2025-04-01", 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("durée nulle : attendu ErrValidation, obtenu %v", err)
	}
	if _, err := FinLease("2025-04-01", -12); !errors.Is(err, ErrValidation) {
		t.Fatalf("durée négative : attendu ErrValidation, obtenu %v", err)
	}
}
