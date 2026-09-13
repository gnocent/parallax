package depot

import (
	"errors"
	"testing"
)

// serveurRevDeTest sème le catalogue, crée un serveur et renvoie (serveurID,
// rev1, rev2).
func serveurRevDeTest(t *testing.T) (*Depot, int64, int64, int64) {
	t.Helper()
	d := depotDeTest(t)
	semerRefs(t, d.base)
	_, rev1, rev2 := semerCatalogue(t, d.base)
	s, err := d.CreerServeur(Serveur{PhysicalName: ptrStr("PHY"), Statut: StatutEnService})
	if err != nil {
		t.Fatalf("serveur : %v", err)
	}
	return d, s.ID, rev1, rev2
}

func TestRattacherRevisionCloture(t *testing.T) {
	d, srv, rev1, rev2 := serveurRevDeTest(t)

	if err := d.RattacherRevision(srv, rev1, "2020-07-01", nil); err != nil {
		t.Fatalf("premier rattachement : %v", err)
	}
	if err := d.RattacherRevision(srv, rev2, "2024-03-01", ptrStr("ajout SSD")); err != nil {
		t.Fatalf("second rattachement : %v", err)
	}

	list, err := d.ListerRattachements(srv)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("attendu 2 rattachements, obtenu %d", len(list))
	}
	if list[0].DateFin == nil || *list[0].DateFin != "2024-02-29" {
		t.Fatalf("la période précédente doit finir à J-1 (2024-02-29) : %+v", list[0])
	}
	if list[1].DateFin != nil || list[1].RevisionID != rev2 {
		t.Fatalf("la nouvelle période doit être ouverte : %+v", list[1])
	}

	rev, err := d.RevisionADate(srv, "2024-03-01")
	if err != nil || rev != rev2 {
		t.Fatalf("révision au 2024-03-01 : %d / %v", rev, err)
	}
}

func TestRattacherRevisionDateAnterieure(t *testing.T) {
	d, srv, rev1, rev2 := serveurRevDeTest(t)
	if err := d.RattacherRevision(srv, rev1, "2020-07-01", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.RattacherRevision(srv, rev2, "2020-07-01", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("date non postérieure : attendu ErrValidation, obtenu %v", err)
	}
	if err := d.RattacherRevision(srv, rev2, "2019-01-01", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("date antérieure : attendu ErrValidation, obtenu %v", err)
	}
}

func TestRattacherRevisionChevauchementFerme(t *testing.T) {
	d, srv, rev1, rev2 := serveurRevDeTest(t)
	// période fermée déjà en base, insérée directement
	exec(t, d.base,
		`INSERT INTO serveur_revision (serveur_id, revision_id, date_debut, date_fin)
		 VALUES (?, ?, '2020-01-01', '2020-12-31')`, srv, rev1)

	err := d.RattacherRevision(srv, rev2, "2020-06-01", nil)
	if !errors.Is(err, ErrChevauchement) {
		t.Fatalf("recouvrement d'une période fermée : attendu ErrChevauchement, obtenu %v", err)
	}
}

func TestCloturerRattachement(t *testing.T) {
	d, srv, rev1, _ := serveurRevDeTest(t)
	if err := d.RattacherRevision(srv, rev1, "2020-07-01", nil); err != nil {
		t.Fatal(err)
	}
	list, _ := d.ListerRattachements(srv)
	id := list[0].ID

	if err := d.CloturerRattachement(id, "2019-01-01"); !errors.Is(err, ErrValidation) {
		t.Fatalf("fin antérieure au début : attendu ErrValidation, obtenu %v", err)
	}
	if err := d.CloturerRattachement(id, "2024-02-29"); err != nil {
		t.Fatalf("clôture : %v", err)
	}
	list, _ = d.ListerRattachements(srv)
	if list[0].DateFin == nil || *list[0].DateFin != "2024-02-29" {
		t.Fatalf("clôture non persistée : %+v", list[0])
	}
	if err := d.CloturerRattachement(404, "2024-02-29"); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("id inconnu : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestCloturerRattachementChevauchement(t *testing.T) {
	d, srv, rev1, rev2 := serveurRevDeTest(t)
	// une période ouverte et une période fermée postérieure, insérées directement
	exec(t, d.base,
		`INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, ?, '2020-01-01')`,
		srv, rev1)
	exec(t, d.base,
		`INSERT INTO serveur_revision (serveur_id, revision_id, date_debut, date_fin)
		 VALUES (?, ?, '2025-01-01', '2026-01-01')`, srv, rev2)

	list, _ := d.ListerRattachements(srv)
	ouverteID := list[0].ID // 2020-01-01, ouverte

	err := d.CloturerRattachement(ouverteID, "2025-06-01")
	if !errors.Is(err, ErrChevauchement) {
		t.Fatalf("clôture créant un recouvrement : attendu ErrChevauchement, obtenu %v", err)
	}
}

func TestRevisionADate(t *testing.T) {
	d, srv, rev1, rev2 := serveurRevDeTest(t)
	_ = d.RattacherRevision(srv, rev1, "2020-07-01", nil)
	_ = d.RattacherRevision(srv, rev2, "2024-03-01", nil)

	if r, _ := d.RevisionADate(srv, "2023-01-01"); r != rev1 {
		t.Fatalf("au 2023-01-01 attendu rev1, obtenu %d", r)
	}
	if r, _ := d.RevisionADate(srv, "2024-02-29"); r != rev1 {
		t.Fatalf("borne haute inclusive : attendu rev1, obtenu %d", r)
	}
	if r, _ := d.RevisionADate(srv, "2024-03-01"); r != rev2 {
		t.Fatalf("borne basse inclusive : attendu rev2, obtenu %d", r)
	}
	if _, err := d.RevisionADate(srv, "2019-01-01"); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("avant tout rattachement : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestRevisionDateMoinsUnJour(t *testing.T) {
	got, err := dateMoinsUnJour("2024-03-01")
	if err != nil || got != "2024-02-29" {
		t.Fatalf("2024-03-01 - 1j attendu 2024-02-29 (année bissextile), obtenu %q / %v", got, err)
	}
	if _, err := dateMoinsUnJour("pas une date"); !errors.Is(err, ErrValidation) {
		t.Fatalf("entrée invalide : attendu ErrValidation, obtenu %v", err)
	}
}
