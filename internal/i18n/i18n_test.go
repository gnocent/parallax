package i18n

import (
	"sort"
	"testing"
)

// TestCataloguesComplets refuse toute divergence entre les catalogues : une
// clé ajoutée à une langue doit l'être aux deux, dans le même Ajouter().
func TestCataloguesComplets(t *testing.T) {
	absentesEN, absentesFR := clesManquantes()
	sort.Strings(absentesEN)
	sort.Strings(absentesFR)
	if len(absentesEN) > 0 {
		t.Errorf("clés françaises absentes du catalogue anglais : %v", absentesEN)
	}
	if len(absentesFR) > 0 {
		t.Errorf("clés anglaises absentes du catalogue français : %v", absentesFR)
	}
}

func TestT(t *testing.T) {
	if T(FR, "commun.modifier") != "Modifier" {
		t.Fatal("résolution directe en français")
	}
	if T(EN, "commun.modifier") != "Edit" {
		t.Fatal("résolution directe en anglais")
	}
	if got := T(EN, "cle-totalement-inconnue"); got != "!cle-totalement-inconnue!" {
		t.Fatalf("une clé inconnue doit être visible, pas silencieuse : %q", got)
	}
}

func TestEstValide(t *testing.T) {
	if l, ok := EstValide("EN"); !ok || l != EN {
		t.Fatalf("EN doit être accepté insensible à la casse : %v %v", l, ok)
	}
	if l, ok := EstValide("xx"); ok || l != Defaut {
		t.Fatalf("une langue inconnue retombe sur Defaut sans être valide : %v %v", l, ok)
	}
}

func TestAutre(t *testing.T) {
	if FR.Autre() != EN || EN.Autre() != FR {
		t.Fatal("Autre() doit alterner entre les deux seules langues supportées")
	}
}

func TestAjouterPaniqueSurDoublon(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("une clé déjà déclarée doit paniquer, pas s'écraser silencieusement")
		}
	}()
	Ajouter(map[string]string{"commun.modifier": "x"}, map[string]string{"commun.modifier": "x"})
}
