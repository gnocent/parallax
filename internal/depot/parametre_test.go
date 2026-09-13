package depot

import (
	"errors"
	"testing"
)

func TestParametreAbsentPuisDefiniPuisRemplace(t *testing.T) {
	d := depotDeTest(t)

	valeur, present, err := d.LireParametre(ParametreGabaritDemande)
	if err != nil {
		t.Fatal(err)
	}
	if present || valeur != "" {
		t.Fatalf("un paramètre jamais défini doit être absent, obtenu present=%v valeur=%q", present, valeur)
	}

	if err := d.DefinirParametre(ParametreGabaritDemande, "Serveur {nom_physique}", nil); err != nil {
		t.Fatal(err)
	}
	valeur, present, err = d.LireParametre(ParametreGabaritDemande)
	if err != nil {
		t.Fatal(err)
	}
	if !present || valeur != "Serveur {nom_physique}" {
		t.Fatalf("attendu la valeur définie, obtenu present=%v valeur=%q", present, valeur)
	}

	// remplacement (upsert) avec un auteur : la ligne est mise à jour, pas
	// dupliquée, et l'audit suit.
	u, err := d.CreerUtilisateur(Utilisateur{Login: "admin", Hash: "x", Role: RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DefinirParametre(ParametreGabaritDemande, "v2", &u.ID); err != nil {
		t.Fatal(err)
	}
	var nb int
	var modifiePar *int64
	var modifieLe string
	if err := d.base.QueryRow(
		`SELECT COUNT(*), MAX(modifie_par), MAX(modifie_le) FROM parametre WHERE cle = ?`,
		ParametreGabaritDemande).Scan(&nb, &modifiePar, &modifieLe); err != nil {
		t.Fatal(err)
	}
	if nb != 1 {
		t.Fatalf("l'upsert doit conserver une seule ligne, trouvé %d", nb)
	}
	if modifiePar == nil || *modifiePar != u.ID || modifieLe == "" {
		t.Fatalf("l'audit doit porter l'auteur et l'horodatage, obtenu %v %q", modifiePar, modifieLe)
	}
	valeur, _, _ = d.LireParametre(ParametreGabaritDemande)
	if valeur != "v2" {
		t.Fatalf("attendu « v2 », obtenu %q", valeur)
	}
}

func TestParametreCleVideRefusee(t *testing.T) {
	d := depotDeTest(t)
	if err := d.DefinirParametre("  ", "x", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("clé vide : attendu ErrValidation, obtenu %v", err)
	}
	if _, _, err := d.LireParametre(""); !errors.Is(err, ErrValidation) {
		t.Fatalf("clé vide : attendu ErrValidation, obtenu %v", err)
	}
}

func TestParametreAuteurInexistantRefuse(t *testing.T) {
	d := depotDeTest(t)
	inconnu := int64(9999)
	if err := d.DefinirParametre("cle", "x", &inconnu); !errors.Is(err, ErrReference) {
		t.Fatalf("auteur inexistant : attendu ErrReference, obtenu %v", err)
	}
}
