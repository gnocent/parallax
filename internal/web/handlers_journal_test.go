package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"parallax/internal/auth"
	"parallax/internal/depot"
)

// TestJournalPorteLAuteurDesEcrituresWeb : une écriture faite depuis un
// écran est journalisée au nom de l'utilisateur connecté (s.depotPour), la
// page /journal (administrateur) la montre avec ses états, et le fragment
// historique d'une fiche la retrouve.
func TestJournalPorteLAuteurDesEcrituresWeb(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur) // gnocent, éditeur
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/projets")

	req, err := http.NewRequest(http.MethodPost, serveur.URL+"/projets",
		strings.NewReader(url.Values{"code": {"LOGS"}, "libelle": {"Plateforme de logs"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jetonCSRF)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("création du projet : attendu 200, obtenu %d : %s", rep.StatusCode, corps(t, rep))
	}

	entrees, err := d.ListerJournal(depot.FiltreJournal{Entite: "projet"})
	if err != nil || len(entrees) != 1 {
		t.Fatalf("attendu une entrée projet dans le journal, obtenu %d (%v)", len(entrees), err)
	}
	e := entrees[0]
	if e.Action != depot.ActionCreation || e.UtilisateurLogin == nil || *e.UtilisateurLogin != "gnocent" {
		t.Fatalf("l'entrée doit être une création signée gnocent : %+v", e)
	}
	if e.Apres == nil || !strings.Contains(*e.Apres, `"Code":"LOGS"`) {
		t.Fatalf("état après attendu avec le code du projet : %+v", e)
	}

	// le fragment historique de la fiche est accessible à tout lecteur
	repHist, err := client.Get(serveur.URL + "/journal/historique/projet/" + itoa(e.EntiteID))
	if err != nil {
		t.Fatal(err)
	}
	if frag := corps(t, repHist); repHist.StatusCode != http.StatusOK || !strings.Contains(frag, "gnocent") || !strings.Contains(frag, "CREATION") {
		t.Fatalf("fragment historique inattendu (%d) : %s", repHist.StatusCode, frag)
	}

	// la page globale est réservée aux administrateurs
	if rep, _ := client.Get(serveur.URL + "/journal"); rep.StatusCode != http.StatusForbidden {
		t.Fatalf("/journal doit être refusée à un éditeur, obtenu %d", rep.StatusCode)
	}
	hash, _ := auth.HacherMotDePasse("s3cret!")
	if _, err := d.CreerUtilisateur(depot.Utilisateur{Login: "admin2", Hash: hash, Role: depot.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	admin := clientConnecteEnTantQue(t, serveur, "admin2", "s3cret!")
	repJournal, err := admin.Get(serveur.URL + "/journal?entite=projet")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repJournal)
	if repJournal.StatusCode != http.StatusOK || !strings.Contains(page, "projet #"+itoa(e.EntiteID)) || !strings.Contains(page, "gnocent") {
		t.Fatalf("page journal inattendue (%d) : %s", repJournal.StatusCode, page)
	}
	if !strings.Contains(page, `href="/journal?entite=projet&amp;export=csv"`) {
		t.Fatalf("liens d'export attendus sur /journal : %s", page)
	}
	repCSV, _ := admin.Get(serveur.URL + "/journal?export=csv")
	if csv := corps(t, repCSV); !strings.HasPrefix(csv, "Horodatage;Auteur;Entité;Identifiant;Action;Avant;Après\n") {
		t.Fatalf("export csv du journal inattendu : %s", csv)
	}
}
