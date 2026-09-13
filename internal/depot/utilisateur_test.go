package depot

import (
	"errors"
	"testing"
)

func TestUtilisateurCycleDeVie(t *testing.T) {
	d := depotDeTest(t)

	u, err := d.CreerUtilisateur(Utilisateur{
		Login: " alice ", Hash: " h1 ", Nom: ptrChaine(" Alice Martin "), Role: RoleEditeur,
	})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if u.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if u.Login != "alice" || u.Hash != "h1" {
		t.Fatalf("champs non normalisés : %+v", u)
	}
	if u.Nom == nil || *u.Nom != "Alice Martin" {
		t.Fatalf("nom non normalisé : %+v", u.Nom)
	}
	if !u.Actif {
		t.Fatal("un compte neuf doit être actif")
	}
	if u.CreeLe == "" {
		t.Fatal("cree_le doit être posé par le dépôt")
	}

	relu, err := d.LireUtilisateur(u.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu.ID != u.ID || relu.Login != u.Login || relu.Hash != u.Hash ||
		relu.Role != u.Role || relu.Actif != u.Actif || relu.CreeLe != u.CreeLe {
		t.Fatalf("relecture divergente : %+v vs %+v", relu, u)
	}
	if relu.Nom == nil || *relu.Nom != "Alice Martin" {
		t.Fatalf("nom non persisté : %+v", relu.Nom)
	}

	parLogin, err := d.LireUtilisateurParLogin("alice")
	if err != nil || parLogin.ID != u.ID {
		t.Fatalf("lecture par login : %v / %+v", err, parLogin)
	}

	// Modification du nom et du rôle.
	u.Nom = ptrChaine("Alice M.")
	u.Role = RoleAdmin
	if err := d.ModifierUtilisateur(u); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireUtilisateur(u.ID)
	if relu.Role != RoleAdmin || relu.Nom == nil || *relu.Nom != "Alice M." {
		t.Fatalf("modification non persistée : %+v", relu)
	}

	// Effacement du nom.
	u.Nom = nil
	if err := d.ModifierUtilisateur(u); err != nil {
		t.Fatalf("modification (nom vidé) : %v", err)
	}
	relu, _ = d.LireUtilisateur(u.ID)
	if relu.Nom != nil {
		t.Fatalf("nom attendu NULL, obtenu %q", *relu.Nom)
	}
}

func TestUtilisateurDefinirHash(t *testing.T) {
	d := depotDeTest(t)
	u, _ := d.CreerUtilisateur(Utilisateur{Login: "bob", Hash: "avant", Role: RoleLecteur})

	if err := d.DefinirHash(u.ID, "  apres  "); err != nil {
		t.Fatalf("définition du hash : %v", err)
	}
	relu, _ := d.LireUtilisateur(u.ID)
	if relu.Hash != "apres" {
		t.Fatalf("hash non persisté : %q", relu.Hash)
	}

	if err := d.DefinirHash(u.ID, "   "); !errors.Is(err, ErrValidation) {
		t.Fatalf("hash vide : attendu ErrValidation, obtenu %v", err)
	}
	if err := d.DefinirHash(404, "x"); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("id absent : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestUtilisateurLoginUnique(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerUtilisateur(Utilisateur{Login: "x", Hash: "h", Role: RoleLecteur}); err != nil {
		t.Fatal(err)
	}
	_, err := d.CreerUtilisateur(Utilisateur{Login: "x", Hash: "h", Role: RoleLecteur})
	if !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
}

func TestUtilisateurValidation(t *testing.T) {
	d := depotDeTest(t)

	for _, cas := range []Utilisateur{
		{Login: "", Hash: "h", Role: RoleLecteur},
		{Login: "  ", Hash: "h", Role: RoleLecteur},
		{Login: "ok", Hash: "", Role: RoleLecteur},
		{Login: "ok", Hash: "  ", Role: RoleLecteur},
	} {
		if _, err := d.CreerUtilisateur(cas); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v : attendu ErrValidation, obtenu %v", cas, err)
		}
	}

	// Rôle non reconnu : c'est la contrainte CHECK de SQLite qui tranche,
	// traduite en ErrValidation par traduire.
	if _, err := d.CreerUtilisateur(Utilisateur{Login: "z", Hash: "h", Role: "ROI"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("rôle invalide : attendu ErrValidation, obtenu %v", err)
	}
	// Idem à la modification.
	u, _ := d.CreerUtilisateur(Utilisateur{Login: "w", Hash: "h", Role: RoleLecteur})
	if err := d.ModifierUtilisateur(Utilisateur{ID: u.ID, Role: "SORCIER"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("rôle invalide (modif) : attendu ErrValidation, obtenu %v", err)
	}
}

func TestUtilisateurIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireUtilisateur(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierUtilisateur(Utilisateur{ID: 404, Role: RoleLecteur}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ArchiverUtilisateur(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("archivage : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestUtilisateurArchivage(t *testing.T) {
	d := depotDeTest(t)
	a, _ := d.CreerUtilisateur(Utilisateur{Login: "aaa", Hash: "h", Role: RoleLecteur})
	b, _ := d.CreerUtilisateur(Utilisateur{Login: "bbb", Hash: "h", Role: RoleLecteur})

	if err := d.ArchiverUtilisateur(a.ID); err != nil {
		t.Fatalf("archivage : %v", err)
	}

	actifs, err := d.ListerUtilisateurs(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(actifs) != 1 || actifs[0].ID != b.ID {
		t.Fatalf("l'archivé ne doit plus figurer dans la liste courante : %+v", actifs)
	}

	tous, _ := d.ListerUtilisateurs(true)
	if len(tous) != 2 || tous[0].Login != "aaa" || tous[1].Login != "bbb" {
		t.Fatalf("liste complète triée par login attendue : %+v", tous)
	}

	if err := d.ReactiverUtilisateur(a.ID); err != nil {
		t.Fatalf("réactivation : %v", err)
	}
	if relu, _ := d.LireUtilisateur(a.ID); !relu.Actif {
		t.Fatal("réactivation non persistée")
	}
}
