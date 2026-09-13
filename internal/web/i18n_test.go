package web

import (
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"parallax/internal/i18n"
)

// clesTraduction repère les appels {{t "cle"}} dans un gabarit. Convention
// fixée par internal/i18n : clé en minuscules, chiffres, points et
// underscores. Un appel qui s'écarterait de cette forme (variable, espace
// dans la clé…) échapperait à ce test — à éviter, d'où la convention.
var clesTraduction = regexp.MustCompile(`\{\{\s*t\s+"([a-z0-9_.]+)"\s*\}\}`)

// TestClesGabaritsDeclarees parcourt tous les gabarits embarqués et vérifie
// que chaque clé de traduction utilisée existe dans les deux catalogues
// (français, anglais). C'est le garde-fou qui permet de répartir la
// traduction des écrans sur plusieurs personnes ou plusieurs passes sans
// double-vérification manuelle : une clé oubliée dans un catalogue, ou mal
// orthographiée dans un gabarit, fait échouer make check immédiatement,
// plutôt que de se découvrir à l'écran en anglais.
func TestClesGabaritsDeclarees(t *testing.T) {
	var manquantesFR, manquantesEN []string
	err := fs.WalkDir(fichiersGabarits, "templates", func(chemin string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(chemin, ".html") {
			return err
		}
		contenu, err := fs.ReadFile(fichiersGabarits, chemin)
		if err != nil {
			return err
		}
		for _, m := range clesTraduction.FindAllStringSubmatch(string(contenu), -1) {
			cle := m[1]
			if v := i18n.T(i18n.FR, cle); strings.HasPrefix(v, "!"+cle) {
				manquantesFR = append(manquantesFR, chemin+" : "+cle)
			}
			if v := i18n.T(i18n.EN, cle); strings.HasPrefix(v, "!"+cle) {
				manquantesEN = append(manquantesEN, chemin+" : "+cle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(manquantesFR)
	sort.Strings(manquantesEN)
	if len(manquantesFR) > 0 {
		t.Errorf("clés absentes du catalogue français :\n  %s", strings.Join(manquantesFR, "\n  "))
	}
	if len(manquantesEN) > 0 {
		t.Errorf("clés absentes du catalogue anglais :\n  %s", strings.Join(manquantesEN, "\n  "))
	}
}

// TestBasculeLangue vérifie le cookie posé par /langue/{lang}, son repli sur
// le français pour une valeur inconnue, et la redirection vers une page
// locale sûre (jamais l'URL du Referer telle quelle).
func TestBasculeLangue(t *testing.T) {
	serveur, _ := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }

	req, _ := http.NewRequest(http.MethodGet, serveur.URL+"/langue/en", nil)
	req.Header.Set("Referer", serveur.URL+"/projets?inclure_inactifs=1")
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") != "/projets?inclure_inactifs=1" {
		t.Fatalf("redirection attendue vers /projets?inclure_inactifs=1, obtenu %d %s", rep.StatusCode, rep.Header.Get("Location"))
	}
	var cookieLang string
	for _, c := range rep.Cookies() {
		if c.Name == cookieLangue {
			cookieLang = c.Value
		}
	}
	if cookieLang != "en" {
		t.Fatalf("cookie de langue attendu à en, obtenu %q", cookieLang)
	}

	pageAnglaise := corps(t, get(t, client, serveur.URL+"/projets"))
	if !strings.Contains(pageAnglaise, "Projects") || strings.Contains(pageAnglaise, ">Projets<") {
		t.Fatalf("l'écran des projets devrait s'afficher en anglais : %s", pageAnglaise[:min(400, len(pageAnglaise))])
	}

	// langue inconnue : repli sur le français, jamais une erreur.
	req2, _ := http.NewRequest(http.MethodGet, serveur.URL+"/langue/xx", nil)
	rep2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.StatusCode != http.StatusSeeOther {
		t.Fatalf("une langue inconnue doit rediriger proprement, obtenu %d", rep2.StatusCode)
	}

	// pas de Referer : retombe sur l'accueil, jamais une redirection vide.
	req3, _ := http.NewRequest(http.MethodGet, serveur.URL+"/langue/fr", nil)
	rep3, err := client.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	if rep3.Header.Get("Location") != "/" {
		t.Fatalf("sans Referer, redirection attendue vers /, obtenu %q", rep3.Header.Get("Location"))
	}
}

func get(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	rep, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}
