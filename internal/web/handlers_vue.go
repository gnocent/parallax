package web

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"parallax/internal/depot"
	"parallax/internal/vues"
	"parallax/internal/xlsx"
)

// routesVues est le constructeur de vues (v1.6) : remplace les tableaux
// croisés dynamiques de l'Excel actuel. Depuis la v2.1, paramétrable par
// scénario (paramètre "scenario", voir scenario_contexte.go) ; depuis la
// v3.0, à une date ("date") ; depuis la v3.3, les colonnes de licences se
// calculent sur le groupe avec les contrats résolus à l'année de cette date
// (internal/vues/licences.go).
func (s *serveur) routesVues() {
	s.mux.HandleFunc("GET /vues", s.lecteur(s.vuesPage))
	s.mux.HandleFunc("GET /vues/resultat", s.lecteur(s.vuesResultatFragment))
	s.mux.HandleFunc("GET /vues/resultat.csv", s.lecteur(s.vuesResultatCSV))
	s.mux.HandleFunc("GET /vues/resultat.xlsx", s.lecteur(s.vuesResultatXLSX))
	s.mux.HandleFunc("POST /vues", s.editeur(s.vuesEnregistrer))
	s.mux.HandleFunc("GET /vues/{id}", s.lecteur(s.vuesOuvrir))
	s.mux.HandleFunc("GET /vues/{id}/resultat.csv", s.lecteur(s.vuesEnregistreeCSV))
	s.mux.HandleFunc("GET /vues/{id}/resultat.xlsx", s.lecteur(s.vuesEnregistreeXLSX))
	s.mux.HandleFunc("POST /vues/{id}/partager", s.editeur(s.vuesBasculerPartage))
	s.mux.HandleFunc("POST /vues/{id}/supprimer", s.editeur(s.vuesSupprimer))
}

// ------------------------------------------------------- page et résultat

func (s *serveur) vuesPage(w http.ResponseWriter, r *http.Request) {
	enregistrees, err := s.vuesAccessibles(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) du tableau « Mes vues » : le résultat du
	// constructeur garde ses exports dédiés (/vues/resultat.csv|xlsx).
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Mes vues", Colonnes: []string{"Nom", "Partagée", "Créée le"}}
		for _, v := range enregistrees {
			t.Lignes = append(t.Lignes, []any{v.Nom, v.Partagee, v.DateCreation})
		}
		return t, nil
	}) {
		return
	}

	lignes, err := vues.ChargerParcCourant(s.depot.Base(), scenarioDepuisRequete(r), dateDepuisRequete(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	selecteur, err := s.chargerSelecteurScenarios(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	s.rendrePage(w, r, s.titre(r, "titre.vues"), "vues_page", map[string]any{
		"Dimensions":    dimensionsAffichables(),
		"Colonnes":      colonnesAffichables(),
		"ValeursFiltre": valeursFiltreParCode(lignes),
		"Vues":          enregistrees,
		"Scenarios":     selecteur.Scenarios,
		"ScenarioID":    selecteur.ScenarioActifID,
		"Date":          dateDepuisRequete(r),
		"Export":        liensExport(r),
	})
}

// vuesAccessibles renvoie les vues que l'utilisateur courant peut voir (les
// siennes, plus celles partagées) — vide s'il n'y a pas d'identité (ne
// devrait pas arriver derrière s.lecteur, mais on ne suppose rien).
func (s *serveur) vuesAccessibles(r *http.Request) ([]depot.Vue, error) {
	identite, ok := identiteDepuis(r.Context())
	if !ok {
		return nil, nil
	}
	return s.depot.ListerVuesAccessibles(identite.UtilisateurID)
}

// resultatAffichable est la forme aplatie d'un Groupe, prête pour le
// gabarit : Valeurs est déjà dans l'ordre des colonnes choisies, pas besoin
// d'indexer une carte depuis le template.
type resultatAffichable struct {
	LibellesAxes     []string
	LibellesColonnes []string
	Lignes           []ligneAffichable
	// NoteGlobal : une colonne de licences est affichée et au moins un
	// contrat résolu compte au niveau GLOBAL — le gabarit rappelle alors que
	// ce niveau n'est pas additif (backlog v3.3).
	NoteGlobal bool
}

type ligneAffichable struct {
	Cles    []string
	Valeurs []float64
}

// calculerResultat lit la configuration du constructeur dans la requête ; une
// vue enregistrée passe par resultatDepuisConfig avec sa propre
// configuration (voir vuesOuvrir et les exports /vues/{id}/resultat.*).
func (s *serveur) calculerResultat(r *http.Request) (resultatAffichable, error) {
	axes, filtre, colonnes := configDepuisRequete(r)
	return s.resultatDepuisConfig(axes, filtre, colonnes, scenarioDepuisRequete(r), dateDepuisRequete(r))
}

// contratsPourVue résout les contrats de licence (v3.3) pour une vue : ceux
// en vigueur l'année de la date de la vue, sous le scénario — seulement si
// une colonne de licences est demandée, sinon nil (rien à calculer).
func (s *serveur) contratsPourVue(colonnes []vues.Colonne, scenarioID *int64, aDate string) (map[int64]depot.LicenceContrat, error) {
	if !vues.DemandeLicences(colonnes) {
		return nil, nil
	}
	return s.depot.ResoudreContrats(anneeDeDate(aDate), scenarioID)
}

// anneeDeDate extrait l'année d'une date YYYY-MM-DD déjà validée
// (dateDepuisRequete) ; une date mal formée, qui ne devrait pas arriver
// jusqu'ici, tombe sur l'année courante plutôt que sur 0.
func anneeDeDate(aDate string) int {
	if t, err := time.Parse("2006-01-02", aDate); err == nil {
		return t.Year()
	}
	return time.Now().Year()
}

// contratGlobalPresent indique si l'un des contrats résolus compte au niveau
// GLOBAL avec un mécanisme où le niveau a un sens (NOEUDS reste une somme,
// donc additif quel que soit le niveau déclaré).
func contratGlobalPresent(contrats map[int64]depot.LicenceContrat) bool {
	for _, c := range contrats {
		if c.Niveau == depot.NiveauLicenceGlobal && c.Mecanisme != depot.MecanismeLicenceNoeuds {
			return true
		}
	}
	return false
}

// resultatDepuisConfig calcule une vue sur le parc du scénario à la date
// aDate (backlog v3.0). Les colonnes de licences (v3.3) passent par
// vues.RegrouperAvecLicences : l'axe techno peut être ajouté en fin, les
// libellés d'en-tête suivent les axes effectifs.
func (s *serveur) resultatDepuisConfig(axes []vues.Dimension, filtre vues.Filtre, colonnes []vues.Colonne, scenarioID *int64, aDate string) (resultatAffichable, error) {
	lignesParc, err := vues.ChargerParcCourant(s.depot.Base(), scenarioID, aDate)
	if err != nil {
		return resultatAffichable{}, err
	}
	contrats, err := s.contratsPourVue(colonnes, scenarioID, aDate)
	if err != nil {
		return resultatAffichable{}, err
	}
	lignesParc = vues.Filtrer(lignesParc, filtre)
	groupes, axesEffectifs := vues.RegrouperAvecLicences(lignesParc, axes, colonnes, contrats)

	res := resultatAffichable{
		LibellesAxes:     libelles(axesEffectifs, vues.LibelleDimension),
		LibellesColonnes: libelles(colonnes, vues.LibelleColonne),
		NoteGlobal:       contratGlobalPresent(contrats),
	}
	for _, g := range groupes {
		lg := ligneAffichable{Cles: g.Cles, Valeurs: make([]float64, len(colonnes))}
		for i, c := range colonnes {
			lg.Valeurs[i] = g.Valeurs[c]
		}
		res.Lignes = append(res.Lignes, lg)
	}
	return res, nil
}

func (s *serveur) vuesResultatFragment(w http.ResponseWriter, r *http.Request) {
	res, err := s.calculerResultat(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "vues_resultat", res)
}

// ----------------------------------------------------------------- export

func (s *serveur) vuesResultatCSV(w http.ResponseWriter, r *http.Request) {
	res, err := s.calculerResultat(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.ecrireCSV(w, res)
}

// vuesEnregistreeCSV et vuesEnregistreeXLSX exportent une vue enregistrée
// avec SA configuration (axes, filtres, colonnes), lue en base — les liens
// d'export de la page d'une vue ne peuvent pas la reconstruire en paramètres
// d'URL depuis le gabarit. Le scénario, lui, reste un paramètre de requête :
// il n'est pas enregistré avec la vue (voir vuesOuvrir).
func (s *serveur) vuesEnregistreeCSV(w http.ResponseWriter, r *http.Request) {
	res, ok := s.resultatVueEnregistree(w, r)
	if !ok {
		return
	}
	s.ecrireCSV(w, res)
}

func (s *serveur) vuesEnregistreeXLSX(w http.ResponseWriter, r *http.Request) {
	res, ok := s.resultatVueEnregistree(w, r)
	if !ok {
		return
	}
	s.ecrireXLSX(w, r, res)
}

// resultatVueEnregistree charge la vue {id} et calcule son résultat ; en cas
// d'erreur, la réponse est déjà écrite et ok est faux.
func (s *serveur) resultatVueEnregistree(w http.ResponseWriter, r *http.Request) (resultatAffichable, bool) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return resultatAffichable{}, false
	}
	v, err := s.depot.LireVue(id)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.NotFound(w, r)
			return resultatAffichable{}, false
		}
		s.erreurServeur(w, r, err)
		return resultatAffichable{}, false
	}
	axes, filtre, colonnes := configDeVue(v)
	res, err := s.resultatDepuisConfig(axes, filtre, colonnes, scenarioDepuisRequete(r), dateDepuisRequete(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return resultatAffichable{}, false
	}
	return res, true
}

// configDeVue décode la configuration enregistrée d'une vue. Un JSON
// illisible (ne devrait pas arriver : c'est nous qui l'avons écrit) donne une
// configuration vide, donc un total général — pas une panne.
func configDeVue(v depot.Vue) ([]vues.Dimension, vues.Filtre, []vues.Colonne) {
	var axes []vues.Dimension
	var colonnes []vues.Colonne
	filtre := vues.Filtre{}
	_ = json.Unmarshal([]byte(v.Axes), &axes)
	_ = json.Unmarshal([]byte(v.Colonnes), &colonnes)
	_ = json.Unmarshal([]byte(v.Filtres), &filtre)
	return axes, filtre, colonnes
}

func (s *serveur) ecrireCSV(w http.ResponseWriter, res resultatAffichable) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="vue.csv"`)

	ew := csv.NewWriter(w)
	ew.Comma = ';' // Excel FR ouvre un CSV point-virgule sans ré-import manuel
	_ = ew.Write(append(append([]string{}, res.LibellesAxes...), res.LibellesColonnes...))
	for _, l := range res.Lignes {
		ligne := append([]string{}, l.Cles...)
		for _, v := range l.Valeurs {
			ligne = append(ligne, formaterNombre(v))
		}
		_ = ew.Write(ligne)
	}
	ew.Flush()
}

func (s *serveur) vuesResultatXLSX(w http.ResponseWriter, r *http.Request) {
	res, err := s.calculerResultat(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.ecrireXLSX(w, r, res)
}

func (s *serveur) ecrireXLSX(w http.ResponseWriter, r *http.Request, res resultatAffichable) {
	entetes := append(append([]string{}, res.LibellesAxes...), res.LibellesColonnes...)
	lignesXLSX := make([][]any, 0, len(res.Lignes))
	for _, l := range res.Lignes {
		ligne := make([]any, 0, len(entetes))
		for _, c := range l.Cles {
			ligne = append(ligne, c)
		}
		for _, v := range l.Valeurs {
			ligne = append(ligne, v)
		}
		lignesXLSX = append(lignesXLSX, ligne)
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="vue.xlsx"`)
	if err := xlsx.Ecrire(w, xlsx.Feuille{Nom: "Vue", Entetes: entetes, Lignes: lignesXLSX}); err != nil {
		// des octets sont peut-être déjà partis (l'archive zip s'écrit en
		// flux) : on ne peut plus changer le code de statut, seulement
		// journaliser.
		s.erreurServeur(w, r, err)
	}
}

// -------------------------------------------------------- vues enregistrées

// vuesEnregistrer sauvegarde la configuration courante du constructeur sous
// un nom. Appelée en htmx (pas un submit natif de formulaire) : la case à
// cocher « inclure les archivés » n'a pas de sens ici, mais le jeton CSRF est
// porté par l'en-tête hx-headers du <body>, pas par un champ caché — pas
// besoin d'en ajouter un dans le gabarit.
func (s *serveur) vuesEnregistrer(w http.ResponseWriter, r *http.Request) {
	identite, _ := identiteDepuis(r.Context())
	axes, filtre, colonnes := configDepuisRequete(r)
	axesJSON, _ := json.Marshal(axes)
	filtreJSON, _ := json.Marshal(filtre)
	colonnesJSON, _ := json.Marshal(colonnes)

	proprietaireID := identite.UtilisateurID
	_, err := s.depotPour(r).CreerVue(depot.Vue{
		Nom:            r.FormValue("nom"),
		ProprietaireID: &proprietaireID,
		Partagee:       r.FormValue("partagee") != "",
		Axes:           string(axesJSON),
		Filtres:        string(filtreJSON),
		Colonnes:       string(colonnesJSON),
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	enregistrees, errListe := s.vuesAccessibles(r)
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "vues_panneau_mes_vues", map[string]any{
		"Vues": enregistrees, "Erreur": messageUtilisateur(err),
	})
}

func (s *serveur) vuesOuvrir(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v, err := s.depot.LireVue(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	axes, filtre, colonnes := configDeVue(v)
	res, err := s.resultatDepuisConfig(axes, filtre, colonnes, scenarioDepuisRequete(r), dateDepuisRequete(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	identite, _ := identiteDepuis(r.Context())
	estProprietaire := v.ProprietaireID != nil && *v.ProprietaireID == identite.UtilisateurID

	// vue_ouverte_page porte ses propres formulaires POST non-htmx
	// (partager/supprimer) : contrairement aux fragments htmx (le jeton CSRF
	// y voyage via l'en-tête hx-headers du <body>), un <form> classique a
	// besoin du jeton en champ caché — donneesPage ne l'expose qu'à
	// mise_en_page, pas au contenu, donc on le republie explicitement ici.
	var csrfToken string
	if session, ok := sessionDepuis(r.Context()); ok {
		csrfToken = session.CSRFToken
	}

	selecteur, err := s.chargerSelecteurScenarios(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, v.Nom, "vue_ouverte_page", map[string]any{
		"Vue": v, "Resultat": res, "EstProprietaire": estProprietaire, "CSRFToken": csrfToken,
		"Scenarios": selecteur.Scenarios, "ScenarioID": selecteur.ScenarioActifID,
		"Date": dateDepuisRequete(r),
	})
}

func (s *serveur) vuesBasculerPartage(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v, err := s.depot.LireVue(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	v.Partagee = !v.Partagee
	if err := s.depotPour(r).ModifierVue(v); err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/vues/%d", id), http.StatusSeeOther)
}

func (s *serveur) vuesSupprimer(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.depotPour(r).SupprimerVue(id); err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	http.Redirect(w, r, "/vues", http.StatusSeeOther)
}

// ---------------------------------------------------------------- helpers

// configDepuisRequete lit axe1/axe2/axe3, filtre_<dimension> (multi-valeurs)
// et colonne (multi-valeurs) depuis la requête (GET query ou POST body,
// r.FormValue couvre les deux). Toute valeur hors énumération est ignorée
// silencieusement plutôt que de faire échouer la requête : un axe ou une
// colonne inconnue ne peut venir que d'une URL bricolée à la main.
func configDepuisRequete(r *http.Request) ([]vues.Dimension, vues.Filtre, []vues.Colonne) {
	_ = r.ParseForm()

	var axes []vues.Dimension
	for i := 1; i <= 3; i++ {
		v := r.FormValue(fmt.Sprintf("axe%d", i))
		if v != "" && dimensionValide(v) {
			axes = append(axes, vues.Dimension(v))
		}
	}

	filtre := vues.Filtre{}
	for _, d := range vues.Dimensions {
		valeurs := r.Form["filtre_"+string(d)]
		if len(valeurs) > 0 {
			filtre[d] = valeurs
		}
	}

	var colonnes []vues.Colonne
	for _, c := range r.Form["colonne"] {
		if colonneValide(c) {
			colonnes = append(colonnes, vues.Colonne(c))
		}
	}

	return axes, filtre, colonnes
}

func dimensionValide(v string) bool {
	for _, d := range vues.Dimensions {
		if string(d) == v {
			return true
		}
	}
	return false
}

func colonneValide(v string) bool {
	for _, c := range vues.Colonnes {
		if string(c) == v {
			return true
		}
	}
	return false
}

func libelles[T ~string](vs []T, libelle func(T) string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = libelle(v)
	}
	return out
}

type optionDimension struct {
	Code    string
	Libelle string
}

func dimensionsAffichables() []optionDimension {
	out := make([]optionDimension, len(vues.Dimensions))
	for i, d := range vues.Dimensions {
		out[i] = optionDimension{Code: string(d), Libelle: vues.LibelleDimension(d)}
	}
	return out
}

func colonnesAffichables() []optionDimension {
	out := make([]optionDimension, len(vues.Colonnes))
	for i, c := range vues.Colonnes {
		out[i] = optionDimension{Code: string(c), Libelle: vues.LibelleColonne(c)}
	}
	return out
}

// valeursFiltreParCode reclé vues.ValeursDisponibles (map[vues.Dimension][]string)
// par le code brut en string : le gabarit indexe cette carte avec le champ
// Code (string) d'un optionDimension, et text/template n'est pas garanti de
// convertir silencieusement un string vers un type nommé pour {{index}}.
func valeursFiltreParCode(lignes []vues.Ligne) map[string][]string {
	source := vues.ValeursDisponibles(lignes)
	out := make(map[string][]string, len(source))
	for d, v := range source {
		out[string(d)] = v
	}
	return out
}
