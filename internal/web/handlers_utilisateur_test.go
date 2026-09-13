package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"parallax/internal/depot"
)

func TestUtilisateursReserveAuxAdmins(t *testing.T) {
	serveur, d := serveurDeTest(t)
	// serveurDeTest crée "gnocent" en EDITEUR ; on vérifie le refus d'accès
	client := clientConnecte(t, serveur)

	rep, err := client.Get(serveur.URL + "/utilisateurs")
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusForbidden {
		t.Fatalf("un EDITEUR doit être refusé sur /utilisateurs : attendu 403, obtenu %d", rep.StatusCode)
	}

	// promotion en ADMIN : l'accès doit maintenant fonctionner
	us, _ := d.ListerUtilisateurs(false)
	us[0].Role = depot.RoleAdmin
	if err := d.ModifierUtilisateur(us[0]); err != nil {
		t.Fatal(err)
	}
	// la session en cours relit le rôle à chaque requête (voir auth.Service.Courant)
	rep2, err := client.Get(serveur.URL + "/utilisateurs")
	if err != nil {
		t.Fatal(err)
	}
	defer rep2.Body.Close()
	if rep2.StatusCode != http.StatusOK {
		t.Fatalf("un ADMIN doit accéder à /utilisateurs : attendu 200, obtenu %d", rep2.StatusCode)
	}
}

func TestUtilisateursCreationEtReinitialisationMotDePasse(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	us, _ := d.ListerUtilisateurs(false)
	us[0].Role = depot.RoleAdmin
	d.ModifierUtilisateur(us[0])

	repPage, err := client.Get(serveur.URL + "/utilisateurs")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	i := strings.Index(page, `X-CSRF-Token":"`) + len(`X-CSRF-Token":"`)
	jeton := page[i : strings.Index(page[i:], `"`)+i]

	// création avec un mot de passe trop court côté hachage n'est pas
	// validée ici (le paquet auth ne fixe pas de longueur minimale) ; on
	// vérifie surtout le chemin heureux et le hachage réel.
	form := url.Values{"login": {"nouveau"}, "role": {depot.RoleLecteur}, "mot_de_passe": {"un-mot-de-passe-correct"}}
	req, _ := http.NewRequest(http.MethodPost, serveur.URL+"/utilisateurs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", rep.StatusCode)
	}
	if !strings.Contains(corps(t, rep), "nouveau") {
		t.Fatal("le nouveau compte devrait apparaître dans le tableau")
	}

	nouveau, err := d.LireUtilisateurParLogin("nouveau")
	if err != nil {
		t.Fatal(err)
	}
	if nouveau.Hash == "un-mot-de-passe-correct" {
		t.Fatal("le mot de passe doit être haché, jamais stocké en clair")
	}

	// réinitialisation du mot de passe
	form2 := url.Values{"mot_de_passe": {"autre-mot-de-passe"}}
	req2, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/utilisateurs/%d/mot-de-passe", serveur.URL, nouveau.ID),
		strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("X-CSRF-Token", jeton)
	rep2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.StatusCode != http.StatusOK {
		t.Fatalf("réinitialisation : attendu 200, obtenu %d", rep2.StatusCode)
	}
	relu, _ := d.LireUtilisateur(nouveau.ID)
	if relu.Hash == nouveau.Hash {
		t.Fatal("le hash aurait dû changer après réinitialisation")
	}
}
