package web

import "testing"

func TestCacheImportDeposerRecupererConsommer(t *testing.T) {
	c := &cacheImport{entrees: map[string]entreeCacheImport{}}
	jeton, err := c.Deposer([]byte("code;libelle\nA;a\n"))
	if err != nil {
		t.Fatal(err)
	}
	contenu, ok := c.Recuperer(jeton)
	if !ok || string(contenu) != "code;libelle\nA;a\n" {
		t.Fatalf("récupération : ok=%v contenu=%q", ok, contenu)
	}

	c.Consommer(jeton)
	if _, ok := c.Recuperer(jeton); ok {
		t.Fatal("un jeton consommé ne doit plus être récupérable")
	}
}

func TestCacheImportJetonInconnu(t *testing.T) {
	c := &cacheImport{entrees: map[string]entreeCacheImport{}}
	if _, ok := c.Recuperer("inexistant"); ok {
		t.Fatal("un jeton inconnu ne doit rien renvoyer")
	}
}
