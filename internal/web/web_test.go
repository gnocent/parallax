package web

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"parallax/internal/auth"
	"parallax/internal/db"
	"parallax/internal/depot"
)

// serveurDeTest démarre un serveur HTTP réel (httptest) sur une base neuve,
// avec un compte connu. Un vrai serveur plutôt que httptest.NewRecorder :
// le cycle connexion → cookie → requêtes protégées par CSRF s'enchaîne comme
// en production, avec un http.Client et son cookiejar.
func serveurDeTest(t *testing.T) (*httptest.Server, *depot.Depot) {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "web-test.db")
	base, err := db.Ouvrir(chemin)
	if err != nil {
		t.Fatalf("ouverture de la base de test : %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	d := depot.Nouveau(base)
	hash, err := auth.HacherMotDePasse("s3cret!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerUtilisateur(depot.Utilisateur{
		Login: "editeur", Hash: hash, Role: depot.RoleEditeur,
	}); err != nil {
		t.Fatal(err)
	}

	svc := auth.NouveauService(d)
	serveur := httptest.NewServer(Nouveau(d, svc))
	t.Cleanup(serveur.Close)
	return serveur, d
}

// clientConnecte renvoie un http.Client avec cookiejar déjà authentifié.
func clientConnecte(t *testing.T, serveur *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	rep, err := client.PostForm(serveur.URL+"/connexion", url.Values{
		"login": {"editeur"}, "mot_de_passe": {"s3cret!"},
	})
	if err != nil {
		t.Fatalf("connexion : %v", err)
	}
	rep.Body.Close()
	if rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("connexion : attendu 303, obtenu %d", rep.StatusCode)
	}
	return client
}

func corps(t *testing.T, rep *http.Response) string {
	t.Helper()
	defer rep.Body.Close()
	b, err := io.ReadAll(rep.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestConnexionEchecIdentifiants(t *testing.T) {
	serveur, _ := serveurDeTest(t)
	rep, err := http.PostForm(serveur.URL+"/connexion", url.Values{
		"login": {"editeur"}, "mot_de_passe": {"mauvais"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusUnauthorized {
		t.Fatalf("attendu 401, obtenu %d", rep.StatusCode)
	}
	if !strings.Contains(corps(t, rep), "Identifiants invalides") {
		t.Fatal("le message d'erreur de connexion doit apparaître dans la page")
	}
}

func TestEcranProjetsExigeConnexion(t *testing.T) {
	serveur, _ := serveurDeTest(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	rep, err := client.Get(serveur.URL + "/projets")
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") != "/connexion?suite=%2Fprojets" {
		t.Fatalf("attendu redirection vers /connexion, obtenu %d %s", rep.StatusCode, rep.Header.Get("Location"))
	}
}

func TestParcoursProjetCompletParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	// la page contient un jeton CSRF valable pour les requêtes suivantes
	repPage, err := client.Get(serveur.URL + "/projets")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	i := strings.Index(page, `X-CSRF-Token":"`)
	if i < 0 {
		t.Fatal("jeton CSRF absent de la page")
	}
	i += len(`X-CSRF-Token":"`)
	jeton := page[i : strings.Index(page[i:], `"`)+i]
	if jeton == "" {
		t.Fatal("jeton CSRF vide")
	}

	// création sans jeton CSRF : refusée
	req, _ := http.NewRequest(http.MethodPost, serveur.URL+"/projets",
		strings.NewReader(url.Values{"code": {"X"}, "libelle": {"x"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	repSansCSRF, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	repSansCSRF.Body.Close()
	if repSansCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("écriture sans CSRF : attendu 403, obtenu %d", repSansCSRF.StatusCode)
	}

	// création avec le jeton : acceptée, la ligne apparaît dans le fragment
	req, _ = http.NewRequest(http.MethodPost, serveur.URL+"/projets",
		strings.NewReader(url.Values{"code": {"LOGS"}, "libelle": {"Log Management"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repCreation, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if fragment := corps(t, repCreation); !strings.Contains(fragment, "LOGS") {
		t.Fatalf("la ligne créée devrait apparaître dans le fragment : %s", fragment)
	}

	projets, err := d.ListerProjets(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(projets) != 1 || projets[0].Code != "LOGS" {
		t.Fatalf("le projet aurait dû être créé en base : %+v", projets)
	}

	// conflit de code : 200 (pas 4xx, htmx n'afficherait rien), message inline
	req, _ = http.NewRequest(http.MethodPost, serveur.URL+"/projets",
		strings.NewReader(url.Values{"code": {"LOGS"}, "libelle": {"doublon"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repConflit, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if repConflit.StatusCode != http.StatusOK {
		t.Fatalf("conflit métier : attendu 200 (htmx n'affiche pas les erreurs 4xx), obtenu %d", repConflit.StatusCode)
	}
	if !strings.Contains(corps(t, repConflit), "existe déjà") {
		t.Fatal("le message de conflit devrait apparaître dans le fragment")
	}
}
