package web

import (
	"net/http"
	"testing"
)

// TestSanteSansAuthentification vérifie que /sante répond sans session — un
// répartiteur de charge ou systemd n'en porte pas.
func TestSanteSansAuthentification(t *testing.T) {
	serveur, _ := serveurDeTest(t)

	rep, err := http.Get(serveur.URL + "/sante")
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("attendu 200, obtenu %d", rep.StatusCode)
	}
	if corps(t, rep) != "ok" {
		t.Fatalf("attendu \"ok\"")
	}
}
