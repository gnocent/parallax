package depot

import (
	"errors"
	"testing"
)

func composantDeBase(revisionID int64) Composant {
	return Composant{
		RevisionID:       revisionID,
		Nature:           "DISQUE_DATA",
		Code:             "ssd",
		Quantite:         24,
		CapaciteUnitaire: 24,
		Unite:            "TO",
	}
}

func TestComposantCycleDeVie(t *testing.T) {
	d := depotDeTest(t)
	m := modeleDeTest(t, d)
	r := revisionDeTest(t, d, m.ID)

	c, err := d.AjouterComposant(composantDeBase(r.ID), false)
	if err != nil {
		t.Fatalf("ajout : %v", err)
	}
	if c.ID == 0 {
		t.Fatal("identifiant non attribué")
	}

	cpu := Composant{RevisionID: r.ID, Nature: "CPU", Code: "cpu", Quantite: 2, CapaciteUnitaire: 32, Unite: "CORE"}
	if _, err := d.AjouterComposant(cpu, false); err != nil {
		t.Fatalf("ajout cpu : %v", err)
	}

	liste, err := d.ListerComposants(r.ID)
	if err != nil {
		t.Fatalf("liste : %v", err)
	}
	if len(liste) != 2 || liste[0].Code != "cpu" || liste[1].Code != "ssd" {
		t.Fatalf("liste non triée par code : %+v", liste)
	}

	c.Quantite = 36
	c.Commentaire = ptrDe("ajout d'un tiroir")
	if err := d.ModifierComposant(c, false); err != nil {
		t.Fatalf("modification : %v", err)
	}
	liste, _ = d.ListerComposants(r.ID)
	if liste[1].Quantite != 36 {
		t.Fatalf("modification non persistée : %+v", liste[1])
	}

	if err := d.SupprimerComposant(c.ID, false); err != nil {
		t.Fatalf("suppression : %v", err)
	}
	if liste, _ := d.ListerComposants(r.ID); len(liste) != 1 {
		t.Fatalf("suppression non effective : %+v", liste)
	}
}

func TestComposantValidation(t *testing.T) {
	d := depotDeTest(t)
	m := modeleDeTest(t, d)
	r := revisionDeTest(t, d, m.ID)

	base := composantDeBase(r.ID)

	vide := base
	vide.Code = "  "
	q0 := base
	q0.Quantite = 0
	cap0 := base
	cap0.CapaciteUnitaire = -1
	for _, cas := range []Composant{vide, q0, cap0} {
		if _, err := d.AjouterComposant(cas, false); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}

	// nature et unité hors énumération : refus par la base, traduit ErrValidation.
	natureKO := base
	natureKO.Nature = "QUANTIQUE"
	if _, err := d.AjouterComposant(natureKO, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("nature invalide : attendu ErrValidation, obtenu %v", err)
	}
	uniteKO := base
	uniteKO.Unite = "FURLONGS"
	if _, err := d.AjouterComposant(uniteKO, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("unité invalide : attendu ErrValidation, obtenu %v", err)
	}
}

func TestComposantCodeUniqueParRevision(t *testing.T) {
	d := depotDeTest(t)
	m := modeleDeTest(t, d)
	r := revisionDeTest(t, d, m.ID)

	if _, err := d.AjouterComposant(composantDeBase(r.ID), false); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AjouterComposant(composantDeBase(r.ID), false); !errors.Is(err, ErrConflit) {
		t.Fatalf("code dupliqué dans la révision : attendu ErrConflit, obtenu %v", err)
	}
}

func TestComposantRevisionInconnue(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.AjouterComposant(composantDeBase(999), false); !errors.Is(err, ErrReference) {
		t.Fatalf("révision inconnue : attendu ErrReference, obtenu %v", err)
	}
}

func TestComposantIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	m := modeleDeTest(t, d)
	r := revisionDeTest(t, d, m.ID)
	fantome := composantDeBase(r.ID)
	fantome.ID = 404
	if err := d.ModifierComposant(fantome, false); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.SupprimerComposant(404, false); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("suppression : attendu ErrIntrouvable, obtenu %v", err)
	}
}

// TestComposantImmuabilite couvre l'invariant 1 pour la composition : dès que la
// révision est référencée, ajout, modification et suppression sont refusés hors
// mode correction.
func TestComposantImmuabilite(t *testing.T) {
	d := depotDeTest(t)
	m := modeleDeTest(t, d)
	r := revisionDeTest(t, d, m.ID)

	c, err := d.AjouterComposant(composantDeBase(r.ID), false)
	if err != nil {
		t.Fatalf("ajout initial : %v", err)
	}

	referencerRevision(t, d, r.ID)

	// Ajout refusé.
	nic := Composant{RevisionID: r.ID, Nature: "NIC", Code: "nic", Quantite: 2, CapaciteUnitaire: 25, Unite: "GBPS"}
	if _, err := d.AjouterComposant(nic, false); !errors.Is(err, ErrImmuable) {
		t.Fatalf("ajout sur révision référencée : attendu ErrImmuable, obtenu %v", err)
	}

	// Modification refusée, sans écriture.
	c.Quantite = 48
	if err := d.ModifierComposant(c, false); !errors.Is(err, ErrImmuable) {
		t.Fatalf("modification : attendu ErrImmuable, obtenu %v", err)
	}
	if liste, _ := d.ListerComposants(r.ID); liste[0].Quantite != 24 {
		t.Fatalf("le refus ne doit rien modifier : %+v", liste[0])
	}

	// Suppression refusée.
	if err := d.SupprimerComposant(c.ID, false); !errors.Is(err, ErrImmuable) {
		t.Fatalf("suppression : attendu ErrImmuable, obtenu %v", err)
	}

	// En mode correction, tout passe.
	if _, err := d.AjouterComposant(nic, true); err != nil {
		t.Fatalf("ajout en correction : %v", err)
	}
	c.Quantite = 48
	if err := d.ModifierComposant(c, true); err != nil {
		t.Fatalf("modification en correction : %v", err)
	}
	if err := d.SupprimerComposant(c.ID, true); err != nil {
		t.Fatalf("suppression en correction : %v", err)
	}
}
