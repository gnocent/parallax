package web

import (
	"errors"
	"net/http"
	"sort"
	"strconv"

	"parallax/internal/depot"
)

// handlers_licence.go est l'écran des contrats de licence (backlog v3.3) :
// un contrat par techno et par année, surchargeable par scénario, consommé
// par les colonnes « Unités de licence » et « Coût licences » du
// constructeur de vues (internal/vues/licences.go). Patron d'édition en
// ligne htmx de handlers_projet.go, avec un sélecteur de scénario comme la
// page de détail d'une variable : la liste montre le réel et les surcharges
// du scénario choisi ensemble (badge « scénario »), un contrat nouveau
// s'écrit dans le scénario choisi, un contrat existant reste dans son seau.
//
// Le scénario voyage en champ caché du formulaire de création (r.FormValue,
// donc lu aussi dans le corps d'un POST — scenarioDepuisRequete) et en
// chaîne de requête des actions de ligne, comme valeur_ligne
// (templates/variables/detail.html).
//
// routesLicences n'est PAS appelée depuis server.go : l'intégration ajoute
// cet appel dans routes() (voir la doc de tête de middleware.go).
func (s *serveur) routesLicences() {
	s.mux.HandleFunc("GET /licences", s.lecteur(s.licencesPage))
	s.mux.HandleFunc("GET /licences/tableau", s.lecteur(s.licencesTableau))
	s.mux.HandleFunc("POST /licences", s.editeur(s.licencesCreer))
	s.mux.HandleFunc("GET /licences/{id}", s.lecteur(s.licencesLigne))
	s.mux.HandleFunc("GET /licences/{id}/editer", s.editeur(s.licencesFormulaireEdition))
	s.mux.HandleFunc("PUT /licences/{id}", s.editeur(s.licencesModifier))
	s.mux.HandleFunc("POST /licences/{id}/supprimer", s.editeur(s.licencesSupprimer))
}

// optionLicence décrit un mécanisme ou un niveau pour les <select> : le code
// persisté, dont le gabarit tire la clé d'aide i18n ("licence.aide.mecanisme.<code>"
// ou "licence.aide.niveau.<code>") — le sens tel qu'implémenté par
// vues.CalculerLicences.
type optionLicence struct {
	Code string
}

var mecanismesLicence = []optionLicence{
	{depot.MecanismeLicenceNoeuds},
	{depot.MecanismeLicenceMaxNoeudsRam},
	{depot.MecanismeLicenceRam},
}

var niveauxLicence = []optionLicence{
	{depot.NiveauLicenceMachine},
	{depot.NiveauLicenceCluster},
	{depot.NiveauLicenceGlobal},
}

// licenceLigne incorpore depot.LicenceContrat et ajoute ce que le gabarit
// affiche : le libellé de la techno, la liste des technos pour l'édition en
// ligne, et un message d'erreur optionnel — même raison que projetLigne
// (handlers_projet.go) : jamais un depot.LicenceContrat nu dans un gabarit.
type licenceLigne struct {
	depot.LicenceContrat
	TechnoCode    string
	TechnoLibelle string
	Technos       []depot.Techno
	Erreur        string
}

// EstDuScenario : badge « scénario » / « réel », comme valeurLigne
// (handlers_variable.go).
func (l licenceLigne) EstDuScenario() bool { return l.ScenarioID != nil }

// ScenarioIDTexte renvoie l'identifiant du seau pour la chaîne de requête
// des actions de ligne (« 0 » pour le réel).
func (l licenceLigne) ScenarioIDTexte() string {
	if l.ScenarioID == nil {
		return "0"
	}
	return strconv.FormatInt(*l.ScenarioID, 10)
}

// RamMaxTexte et CoutTexte exposent les pointeurs comme des chaînes :
// html/template afficherait l'adresse mémoire d'un pointeur.
func (l licenceLigne) RamMaxTexte() string { return flottantTexte(l.RamMaxGo) }
func (l licenceLigne) CoutTexte() string   { return flottantTexte(l.CoutUnitaireHT) }

func flottantTexte(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

// technosParID indexe les technos (actives ou non : un contrat peut viser
// une techno archivée depuis) pour afficher code et libellé sur une ligne.
func (s *serveur) technosParID() ([]depot.Techno, map[int64]depot.Techno, error) {
	technos, err := s.depot.ListerTechnos(true)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(technos, func(i, j int) bool { return technos[i].Code < technos[j].Code })
	index := make(map[int64]depot.Techno, len(technos))
	for _, t := range technos {
		index[t.ID] = t
	}
	return technos, index, nil
}

func (s *serveur) licenceEnLigne(c depot.LicenceContrat, technos []depot.Techno, index map[int64]depot.Techno, erreur string) licenceLigne {
	l := licenceLigne{LicenceContrat: c, Technos: technos, Erreur: erreur}
	if t, ok := index[c.TechnoID]; ok {
		l.TechnoCode, l.TechnoLibelle = t.Code, t.Libelle
	}
	return l
}

// donneesTableauLicences charge les contrats du réel et du scénario choisi,
// et tout ce que le fragment "licences_tableau" attend.
func (s *serveur) donneesTableauLicences(r *http.Request, erreur string) (map[string]any, []licenceLigne, error) {
	scenarioID := scenarioDepuisRequete(r)
	contrats, err := s.depot.ListerLicenceContrats(scenarioID)
	if err != nil {
		return nil, nil, err
	}
	technos, index, err := s.technosParID()
	if err != nil {
		return nil, nil, err
	}
	lignes := make([]licenceLigne, len(contrats))
	for i, c := range contrats {
		lignes[i] = s.licenceEnLigne(c, technos, index, "")
	}
	var scenarioIDOuZero int64
	if scenarioID != nil {
		scenarioIDOuZero = *scenarioID
	}
	return map[string]any{
		"Lignes":     lignes,
		"ScenarioID": scenarioIDOuZero,
		"Erreur":     erreur,
		// les boutons vivent dans le fragment (rechargé par la création et
		// la suppression) et visent la page avec le scénario courant.
		"Export": liensExportVers(r, "/licences", "", "scenario"),
	}, lignes, nil
}

func (s *serveur) licencesPage(w http.ResponseWriter, r *http.Request) {
	donnees, lignes, err := s.donneesTableauLicences(r, "")
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes contrats, même scénario que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Licences", Colonnes: []string{
			"Techno", "Année", "Mécanisme", "Niveau", "RAM max (Go)", "Coût unitaire (HT)", "Commentaire", "Seau",
		}}
		for _, l := range lignes {
			seau := "réel"
			if l.EstDuScenario() {
				seau = "scénario"
			}
			t.Lignes = append(t.Lignes, []any{
				l.TechnoCode, int64(l.Annee), l.Mecanisme, l.Niveau,
				nombreOuVide(l.RamMaxGo), nombreOuVide(l.CoutUnitaireHT), texteOuVide(l.Commentaire), seau,
			})
		}
		return t, nil
	}) {
		return
	}

	selecteur, err := s.chargerSelecteurScenarios(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	technos, err := s.depot.ListerTechnos(false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	donnees["Scenarios"] = selecteur.Scenarios
	donnees["Technos"] = technos
	donnees["Mecanismes"] = mecanismesLicence
	donnees["Niveaux"] = niveauxLicence
	s.rendrePage(w, r, s.titre(r, "titre.licences"), "licences_page", donnees)
}

func (s *serveur) licencesTableau(w http.ResponseWriter, r *http.Request) {
	s.rendreTableauLicences(w, r, "")
}

func (s *serveur) rendreTableauLicences(w http.ResponseWriter, r *http.Request, erreur string) {
	donnees, _, err := s.donneesTableauLicences(r, erreur)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "licences_tableau", donnees)
}

// contratDepuisFormulaire lit les champs communs à la création et à la
// modification. Une valeur numérique illisible redescend en ErrValidation
// (flottantOptionnelModele) : erreur métier affichée, jamais une 400.
func contratDepuisFormulaire(r *http.Request) (depot.LicenceContrat, error) {
	c := depot.LicenceContrat{
		TechnoID:    idRequis(r, "techno_id"),
		Mecanisme:   r.FormValue("mecanisme"),
		Niveau:      r.FormValue("niveau"),
		Commentaire: texteOptionnel(r, "commentaire"),
	}
	c.Annee, _ = strconv.Atoi(r.FormValue("annee")) // 0 est refusé par le dépôt (hors plage)
	var err error
	if c.RamMaxGo, err = flottantOptionnelModele(r, "ram_max_go"); err != nil {
		return c, err
	}
	if c.CoutUnitaireHT, err = flottantOptionnelModele(r, "cout_unitaire_ht"); err != nil {
		return c, err
	}
	return c, nil
}

// messageLicence précise le message générique de messageUtilisateur pour
// l'unicité (techno, année, seau) : « Ce code existe déjà » parlerait d'un
// code que cet écran n'a pas.
func messageLicence(err error) string {
	if errors.Is(err, depot.ErrConflit) {
		return "Un contrat existe déjà pour cette techno et cette année dans ce seau (réel ou scénario)."
	}
	return messageUtilisateur(err)
}

// licencesCreer renvoie toujours le tableau entier (voir projetsCreer) : en
// cas d'erreur métier, inchangé avec un message ; sinon avec la ligne créée.
func (s *serveur) licencesCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	c, err := contratDepuisFormulaire(r)
	if err == nil {
		c.ScenarioID = scenarioDepuisRequete(r)
		_, err = s.depotPour(r).CreerLicenceContrat(c)
	}
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreTableauLicences(w, r, messageLicence(err))
}

func (s *serveur) licencesLigne(w http.ResponseWriter, r *http.Request) {
	s.rendreLigneLicence(w, r, "licences_ligne")
}

func (s *serveur) licencesFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	s.rendreLigneLicence(w, r, "licences_ligne_edition")
}

func (s *serveur) rendreLigneLicence(w http.ResponseWriter, r *http.Request, gabarit string) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := s.depot.LireLicenceContrat(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	technos, index, err := s.technosParID()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, gabarit, s.licenceEnLigne(c, technos, index, ""))
}

func (s *serveur) licencesModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	technos, index, err := s.technosParID()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	c, errForm := contratDepuisFormulaire(r)
	c.ID = id
	errEcriture := errForm
	if errEcriture == nil {
		errEcriture = s.depotPour(r).ModifierLicenceContrat(c)
	}
	if errEcriture != nil {
		if !erreurMetier(errEcriture) {
			s.erreurServeur(w, r, errEcriture)
			return
		}
		// on réaffiche le formulaire d'édition avec l'erreur, l'utilisateur
		// ne perd pas sa saisie ; le seau vient de la base (ModifierLicenceContrat
		// ne le change jamais), pour ne pas afficher un badge faux.
		if actuel, errLecture := s.depot.LireLicenceContrat(id); errLecture == nil {
			c.ScenarioID = actuel.ScenarioID
		}
		s.rendreFragment(w, r, "licences_ligne_edition", s.licenceEnLigne(c, technos, index, messageLicence(errEcriture)))
		return
	}
	relu, err := s.depot.LireLicenceContrat(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "licences_ligne", s.licenceEnLigne(relu, technos, index, ""))
}

// licencesSupprimer retire un contrat puis réaffiche le tableau entier sous
// le scénario de la requête — comme clusterContraintesSupprimer, jamais une
// ligne isolée après une suppression.
func (s *serveur) licencesSupprimer(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = s.depotPour(r).SupprimerLicenceContrat(id)
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreTableauLicences(w, r, messageUtilisateur(err))
}
