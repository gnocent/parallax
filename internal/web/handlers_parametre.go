package web

import (
	"net/http"
	"strings"

	"parallax/internal/depot"
)

// routesParametres enregistre l'édition du gabarit global de description
// des demandes (backlog v3.2, docs/modele-donnees.md §10.1), réservée à un
// administrateur. À câbler dans server.go (routes()) par l'intégration.
//
//	GET  /parametres/gabarit  textarea, aide-mémoire des variables, aperçu
//	POST /parametres/gabarit  valide par CompilerGabarit puis enregistre ;
//	                           refuse avec message inline sinon (200)
func (s *serveur) routesParametres() {
	s.mux.HandleFunc("GET /parametres/gabarit", s.administrateur(s.parametreGabaritPage))
	s.mux.HandleFunc("POST /parametres/gabarit", s.administrateur(s.parametreGabaritEnregistrer))
}

func (s *serveur) parametreGabaritPage(w http.ResponseWriter, r *http.Request) {
	texte, _, err := s.depot.LireParametre(depot.ParametreGabaritDemande)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreGabaritPage(w, r, texte, "", "")
}

// parametreGabaritEnregistrer valide à la saisie : un gabarit qui ne compile
// pas n'est jamais enregistré, et l'administrateur retrouve son texte avec
// le message qui nomme la variable inconnue ou l'accolade orpheline.
func (s *serveur) parametreGabaritEnregistrer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	// un textarea envoie des fins de ligne CRLF : on normalise en LF, pour
	// que le bloc collé n'embarque pas de retour chariot parasite.
	texte := strings.ReplaceAll(r.FormValue("gabarit"), "\r\n", "\n")
	if _, err := CompilerGabarit(texte); err != nil {
		s.rendreGabaritPage(w, r, texte, messageUtilisateur(err), "")
		return
	}
	var auteur *int64
	if identite := identiteOuNil(r); identite != nil {
		auteur = &identite.UtilisateurID
	}
	if err := s.depotPour(r).DefinirParametre(depot.ParametreGabaritDemande, texte, auteur); err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreGabaritPage(w, r, texte, "", "Gabarit enregistré.")
}

// rendreGabaritPage affiche l'éditeur avec, si le texte compile, un aperçu
// rendu sur le premier serveur réel de la base — le plus sûr moyen de voir
// que les variables donnent ce qu'on attend avant d'enregistrer.
func (s *serveur) rendreGabaritPage(w http.ResponseWriter, r *http.Request, texte, erreur, succes string) {
	apercu, apercuServeur := "", ""
	if erreur == "" && strings.TrimSpace(texte) != "" {
		if g, err := CompilerGabarit(texte); err == nil {
			fiches, err := s.depot.ListerFichesDemande(depot.FiltreDemande{})
			if err != nil {
				s.erreurServeur(w, r, err)
				return
			}
			if len(fiches) > 0 {
				apercu = g.Rendre(fiches[0].Valeurs())
				apercuServeur = fiches[0].NomPhysique
			}
		}
	}
	csrf := ""
	if session, ok := sessionDepuis(r.Context()); ok {
		csrf = session.CSRFToken
	}
	s.rendrePage(w, r, s.titre(r, "titre.parametres_gabarit"), "parametres_gabarit_page", map[string]any{
		"Gabarit":       texte,
		"Variables":     VariablesGabarit,
		"Erreur":        erreur,
		"Succes":        succes,
		"Apercu":        apercu,
		"ApercuServeur": apercuServeur,
		"CSRFToken":     csrf,
	})
}
