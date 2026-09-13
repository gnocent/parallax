package web

import (
	"errors"
	"strings"
	"testing"

	"parallax/internal/depot"
)

func TestGabaritRenduTexteVariablesEtLitteraux(t *testing.T) {
	texte := "Serveur {nom_physique} ({hostname})\n" +
		"Cluster : {cluster} — {{modèle}} {modele} rév. {revision}\n" +
		"RAM {ram} Go, SSD {ssd} To, IP {ip}}}"
	g, err := CompilerGabarit(texte)
	if err != nil {
		t.Fatal(err)
	}
	rendu := g.Rendre(map[string]string{
		"nom_physique": "PHY001", "hostname": "esh01", "cluster": "ElasticCold1",
		"modele": "DENSE-2025", "revision": "2", "ram": "1024", "ssd": "184.32",
	})
	attendu := "Serveur PHY001 (esh01)\n" +
		"Cluster : ElasticCold1 — {modèle} DENSE-2025 rév. 2\n" +
		"RAM 1024 Go, SSD 184.32 To, IP }"
	if rendu != attendu {
		t.Fatalf("rendu inattendu :\n%q\nattendu :\n%q", rendu, attendu)
	}

	vars := g.Variables()
	if strings.Join(vars, ",") != "nom_physique,hostname,cluster,modele,revision,ram,ssd,ip" {
		t.Fatalf("variables inattendues : %v", vars)
	}
}

func TestGabaritEspacesDansLesAccoladesEtRenduVide(t *testing.T) {
	g, err := CompilerGabarit("{ cluster } / { ip }")
	if err != nil {
		t.Fatal(err)
	}
	if r := g.Rendre(nil); r != " / " {
		t.Fatalf("valeurs absentes : attendu « / » avec les espaces du texte, obtenu %q", r)
	}
	vide, err := CompilerGabarit("")
	if err != nil {
		t.Fatal(err)
	}
	if vide.Rendre(map[string]string{"cluster": "x"}) != "" {
		t.Fatal("un gabarit vide rend vide")
	}
}

func TestGabaritRefusVariableInconnue(t *testing.T) {
	_, err := CompilerGabarit("Serveur {nom_physique} type {typo}")
	if !errors.Is(err, depot.ErrValidation) {
		t.Fatalf("attendu ErrValidation, obtenu %v", err)
	}
	if !strings.Contains(err.Error(), "« typo »") {
		t.Fatalf("le message doit nommer la variable inconnue : %v", err)
	}
	if _, err := CompilerGabarit("{}"); err == nil || !strings.Contains(err.Error(), "vide") {
		t.Fatalf("une variable vide doit être refusée : %v", err)
	}
}

func TestGabaritRefusAccoladesNonFermees(t *testing.T) {
	cas := map[string]string{
		"Serveur {nom_physique":     "jamais fermée",
		"Serveur {nom_physique\n}":  "jamais fermée",
		"Serveur {nom {cluster}":    "jamais fermée",
		"Serveur nom_physique} fin": "isolée",
	}
	for texte, extrait := range cas {
		_, err := CompilerGabarit(texte)
		if !errors.Is(err, depot.ErrValidation) {
			t.Errorf("%q : attendu ErrValidation, obtenu %v", texte, err)
			continue
		}
		if !strings.Contains(err.Error(), extrait) {
			t.Errorf("%q : le message devrait contenir « %s » : %v", texte, extrait, err)
		}
	}
	// une accolade ouvrante littérale suivie d'une vraie variable reste valide.
	if _, err := CompilerGabarit("{{{cluster}}}"); err != nil {
		t.Fatalf("{{{cluster}}} est valide ({ + variable + }) : %v", err)
	}
}

// TestVariablesGabaritCouvrentLaFiche garantit que le catalogue affiché à
// l'écran et les clés produites par depot.FicheDemande.Valeurs sont le même
// ensemble : une variable acceptée à la saisie doit toujours avoir une
// valeur, et toute valeur disponible doit être documentée.
func TestVariablesGabaritCouvrentLaFiche(t *testing.T) {
	valeurs := depot.FicheDemande{}.Valeurs()
	if len(valeurs) != len(VariablesGabarit) {
		t.Fatalf("catalogue : %d variables, fiche : %d clés", len(VariablesGabarit), len(valeurs))
	}
	for _, v := range VariablesGabarit {
		if _, ok := valeurs[v.Nom]; !ok {
			t.Errorf("la variable « %s » du catalogue n'a pas de valeur dans la fiche", v.Nom)
		}
		if v.Libelle == "" {
			t.Errorf("la variable « %s » n'a pas de libellé", v.Nom)
		}
	}
}
