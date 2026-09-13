package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// clientAdminSauvegarde promeut le compte de test en ADMIN (l'écran de
// sauvegarde lui est réservé, comme /utilisateurs) et renvoie un client déjà
// connecté.
func clientAdminSauvegarde(t *testing.T, serveur *httptest.Server, d *depot.Depot) *http.Client {
	t.Helper()
	us, err := d.ListerUtilisateurs(false)
	if err != nil {
		t.Fatal(err)
	}
	us[0].Role = depot.RoleAdmin
	if err := d.ModifierUtilisateur(us[0]); err != nil {
		t.Fatal(err)
	}
	return clientConnecte(t, serveur)
}

func TestSauvegardeReserveeAuxAdmins(t *testing.T) {
	serveur, _ := serveurDeTest(t)
	client := clientConnecte(t, serveur) // EDITEUR par défaut
	rep, err := client.Get(serveur.URL + "/parametres/sauvegarde")
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusForbidden {
		t.Fatalf("un EDITEUR doit être refusé : attendu 403, obtenu %d", rep.StatusCode)
	}
}

func TestSauvegardePageInitialeDesactivee(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientAdminSauvegarde(t, serveur, d)
	rep, err := client.Get(serveur.URL + "/parametres/sauvegarde")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("attendu 200, obtenu %d : %s", rep.StatusCode, page)
	}
	if !strings.Contains(page, "désactivée") {
		t.Fatalf("sans réglages, l'écran doit annoncer la fonctionnalité désactivée : %s", page)
	}
}

func TestSauvegardeEnregistrementRefuseHeureInvalide(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientAdminSauvegarde(t, serveur, d)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/parametres/sauvegarde")
	dossier := t.TempDir()

	rep, err := client.PostForm(serveur.URL+"/parametres/sauvegarde", url.Values{
		"_csrf": {jeton}, "dossier": {dossier}, "heure": {"pas une heure"}, "retention_jours": {"7"},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	if rep.StatusCode != http.StatusOK || !strings.Contains(page, "Heure invalide") {
		t.Fatalf("heure invalide : attendu un message inline, obtenu %d : %s", rep.StatusCode, page)
	}
	if v, present, _ := d.LireParametre(depot.ParametreSauvegardeHeure); present && v != "" {
		t.Fatalf("rien ne doit être enregistré après une saisie invalide, obtenu %q", v)
	}
}

func TestSauvegardeEnregistrementRefuseRetentionInvalide(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientAdminSauvegarde(t, serveur, d)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/parametres/sauvegarde")

	for _, retention := range []string{"0", "-3", "pas-un-nombre", ""} {
		rep, err := client.PostForm(serveur.URL+"/parametres/sauvegarde", url.Values{
			"_csrf": {jeton}, "dossier": {t.TempDir()}, "heure": {"02:00"}, "retention_jours": {retention},
		})
		if err != nil {
			t.Fatal(err)
		}
		page := corps(t, rep)
		if !strings.Contains(page, "rétention") {
			t.Fatalf("rétention %q : message d'erreur attendu, obtenu : %s", retention, page)
		}
	}
}

func TestSauvegardeEnregistrementValideCreeLeDossierEtPersiste(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientAdminSauvegarde(t, serveur, d)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/parametres/sauvegarde")
	dossier := filepath.Join(t.TempDir(), "nouveau", "sauvegardes")

	rep, err := client.PostForm(serveur.URL+"/parametres/sauvegarde", url.Values{
		"_csrf": {jeton}, "dossier": {dossier}, "heure": {"02:30"}, "retention_jours": {"14"},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	if rep.StatusCode != http.StatusOK || !strings.Contains(page, "Réglages enregistrés") {
		t.Fatalf("attendu succès : %d : %s", rep.StatusCode, page)
	}
	if !strings.Contains(page, "active") {
		t.Fatalf("réglages complets : l'écran doit annoncer la fonctionnalité active : %s", page)
	}
	if _, err := os.Stat(dossier); err != nil {
		t.Fatalf("le dossier de destination doit être créé à l'enregistrement : %v", err)
	}

	heure, present, err := d.LireParametre(depot.ParametreSauvegardeHeure)
	if err != nil || !present || heure != "02:30" {
		t.Fatalf("heure non persistée : %q, %v, %v", heure, present, err)
	}
	retention, _, _ := d.LireParametre(depot.ParametreSauvegardeRetentionJours)
	if retention != "14" {
		t.Fatalf("rétention non persistée : %q", retention)
	}
}

func TestSauvegardeMaintenantSansDossierConfigure(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientAdminSauvegarde(t, serveur, d)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/parametres/sauvegarde")

	rep, err := client.PostForm(serveur.URL+"/parametres/sauvegarde/maintenant", url.Values{"_csrf": {jeton}})
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	if !strings.Contains(page, "Renseignez et enregistrez d") {
		t.Fatalf("dossier absent : message attendu, obtenu : %s", page)
	}
}

func TestSauvegardeMaintenantEcritUnFichierEtApparaitDansLaListe(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientAdminSauvegarde(t, serveur, d)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/parametres/sauvegarde")
	dossier := t.TempDir()

	if _, err := client.PostForm(serveur.URL+"/parametres/sauvegarde", url.Values{
		"_csrf": {jeton}, "dossier": {dossier}, "heure": {"03:00"}, "retention_jours": {"30"},
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := client.PostForm(serveur.URL+"/parametres/sauvegarde/maintenant", url.Values{"_csrf": {jeton}})
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	if !strings.Contains(page, "Sauvegarde écrite") {
		t.Fatalf("attendu confirmation d'écriture, obtenu : %s", page)
	}
	if !strings.Contains(page, ".db") {
		t.Fatalf("le fichier écrit devrait apparaître dans la liste : %s", page)
	}

	entrées, err := os.ReadDir(dossier)
	if err != nil {
		t.Fatal(err)
	}
	if len(entrées) != 1 || !strings.HasSuffix(entrées[0].Name(), ".db") {
		t.Fatalf("attendu un fichier .db dans le dossier, obtenu %+v", entrées)
	}

	derniereReussite, present, _ := d.LireParametre(depot.ParametreSauvegardeDerniereReussite)
	if !present || derniereReussite == "" {
		t.Fatal("la date de dernière réussite doit être enregistrée après une sauvegarde manuelle")
	}
}
