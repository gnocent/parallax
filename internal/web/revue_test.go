package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// Tests des corrections de la revue globale du 2026-09-12.

// TestImportServeursRefuseLesDoublonsDeNom : un nom physique ou un hôte
// répété dans le fichier, ou déjà présent en base, est une erreur de ligne
// qui nomme la cause — plus une violation d'index anonyme à l'écriture.
func TestImportServeursRefuseLesDoublonsDeNom(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/serveurs")
	nom, hote := "PHY-EXISTANT", "esh-existant"
	if _, err := d.CreerServeur(depot.Serveur{PhysicalName: &nom, Hostname: &hote, Statut: depot.StatutEnService, DateEntree: ptrTexte("2026-01-01")}); err != nil {
		t.Fatal(err)
	}
	csv := "physical_name;hostname;statut;date_entree\n" +
		"PHY-A;esh-a;COMMANDE;2026-01-01\n" +
		"PHY-A;esh-b;COMMANDE;2026-01-01\n" +
		"PHY-EXISTANT;esh-c;COMMANDE;2026-01-01\n" +
		"PHY-D;esh-existant;COMMANDE;2026-01-01\n"
	frag := posterCSVImport(t, client, serveur, jetonCSRF, "serveurs", csv)
	for _, attendu := range []string{
		"3 ligne(s) en erreur sur 4",
		"Ligne 3 : physical_name « PHY-A » déjà présent à la ligne 2",
		"Ligne 4 : physical_name « PHY-EXISTANT » existe déjà",
		"Ligne 5 : hostname « esh-existant » existe déjà",
	} {
		if !strings.Contains(frag, attendu) {
			t.Fatalf("attendu « %s » : %s", attendu, frag)
		}
	}
	if ids, _ := d.ServeursParCle("physical_name", "PHY-A"); len(ids) != 0 {
		t.Fatal("un fichier en erreur ne doit rien écrire")
	}
}

// TestFreinConnexion : au-delà du seuil d'échecs sur un login, la
// connexion est refusée en 429 sans vérifier le mot de passe ; un succès
// remet le compteur à zéro.
func TestFreinConnexion(t *testing.T) {
	serveur, _ := serveurDeTest(t)
	freinConnexion.succes("gnocent") // état propre, le frein est global au paquet
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	poster := func(mdp string) int {
		rep, err := client.PostForm(serveur.URL+"/connexion", url.Values{"login": {"gnocent"}, "mot_de_passe": {mdp}})
		if err != nil {
			t.Fatal(err)
		}
		rep.Body.Close()
		return rep.StatusCode
	}
	for i := 0; i < seuilEchecsConnexion; i++ {
		if code := poster("faux"); code != http.StatusUnauthorized {
			t.Fatalf("échec %d : attendu 401, obtenu %d", i+1, code)
		}
	}
	if code := poster("s3cret!"); code != http.StatusTooManyRequests {
		t.Fatalf("après %d échecs, même le bon mot de passe doit être freiné (429), obtenu %d", seuilEchecsConnexion, code)
	}
	freinConnexion.succes("gnocent")
	if code := poster("s3cret!"); code != http.StatusSeeOther {
		t.Fatalf("compteur remis à zéro : connexion attendue (303), obtenu %d", code)
	}
}
