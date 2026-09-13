package depot

import (
	"errors"
	"testing"
)

func TestVueCycleDeVie(t *testing.T) {
	d := depotDeTest(t)
	proprio, err := d.CreerUtilisateur(Utilisateur{Login: "a", Hash: "x", Role: RoleEditeur})
	if err != nil {
		t.Fatal(err)
	}
	autre, err := d.CreerUtilisateur(Utilisateur{Login: "b", Hash: "x", Role: RoleLecteur})
	if err != nil {
		t.Fatal(err)
	}

	v, err := d.CreerVue(Vue{
		Nom: "Capacité par techno", ProprietaireID: &proprio.ID,
		Axes: `["techno"]`, Filtres: `{}`, Colonnes: `["nb_serveurs"]`,
	})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if v.Partagee {
		t.Fatal("une vue neuve n'est pas partagée par défaut")
	}

	relue, err := d.LireVue(v.ID)
	if err != nil || relue.Axes != `["techno"]` {
		t.Fatalf("relecture : %v / %+v", err, relue)
	}

	// non partagée : invisible pour un autre utilisateur
	vues, _ := d.ListerVuesAccessibles(autre.ID)
	if len(vues) != 0 {
		t.Fatalf("vue non partagée visible par un autre utilisateur : %+v", vues)
	}
	vues, _ = d.ListerVuesAccessibles(proprio.ID)
	if len(vues) != 1 {
		t.Fatalf("le propriétaire doit voir sa propre vue : %+v", vues)
	}

	v.Partagee = true
	v.Nom = "Capacité par techno (partagée)"
	if err := d.ModifierVue(v); err != nil {
		t.Fatalf("modification : %v", err)
	}
	vues, _ = d.ListerVuesAccessibles(autre.ID)
	if len(vues) != 1 || vues[0].Nom != "Capacité par techno (partagée)" {
		t.Fatalf("vue partagée devrait être visible par tous : %+v", vues)
	}

	if err := d.SupprimerVue(v.ID); err != nil {
		t.Fatalf("suppression : %v", err)
	}
	if _, err := d.LireVue(v.ID); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("après suppression : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestVueNomObligatoire(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerVue(Vue{Nom: "  "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("attendu ErrValidation, obtenu %v", err)
	}
}
