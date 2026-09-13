package web

import (
	"encoding/csv"
	"math"
	"net/http"
	"sort"
	"strings"

	"parallax/internal/depot"
	"parallax/internal/vues"
	"parallax/internal/xlsx"
)

// handlers_comparaison.go est l'écran de comparaison côte à côte de deux
// scénarios (v2.4 du backlog) : reprend telle quelle la mécanique du
// constructeur de vues (handlers_vue.go — axes, filtres, colonnes,
// configDepuisRequete, dimensionsAffichables, colonnesAffichables,
// valeursFiltreParCode, libelles, formaterNombre — tous réutilisés depuis là,
// jamais dupliqués), appliquée à deux scénarios A et B choisis
// indépendamment plutôt qu'un seul. L'utilisateur choisit lui-même les
// agrégats à comparer (décision de cadrage v2.4) ; les deux colonnes de coût
// (vues.ColCoutAcquisition et vues.ColCoutAnnuel) sont disponibles comme
// n'importe quelle autre colonne, et depuis la v3.3 les deux colonnes de
// licences, calculées par groupe avec les contrats résolus de chaque côté.
//
// Convention d'URL propre à cet écran : "scenario_a" et "scenario_b" (au
// lieu du "scenario" unique de scenario_contexte.go), chacun 0 ou absent ->
// réel. Écran de lecture pure : s.lecteur partout, aucune écriture, donc pas
// de vérification CSRF ni de vue enregistrée ici.
//
// routesComparaison n'est PAS appelée depuis server.go : l'intégration
// ajoute cet appel séparément dans routes() (voir la doc de tête de ce
// fichier commun, jamais modifiée par un écran).
func (s *serveur) routesComparaison() {
	s.mux.HandleFunc("GET /comparaison", s.lecteur(s.comparaisonPage))
	s.mux.HandleFunc("GET /comparaison/resultat", s.lecteur(s.comparaisonResultatFragment))
	s.mux.HandleFunc("GET /comparaison/resultat.csv", s.lecteur(s.comparaisonResultatCSV))
	s.mux.HandleFunc("GET /comparaison/resultat.xlsx", s.lecteur(s.comparaisonResultatXLSX))
}

// scenarioDepuisChamp lit un champ de formulaire désignant un scénario :
// absent, vide ou "0" -> réel (nil). Une valeur non numérique est traitée
// comme absente plutôt que de faire échouer la requête — même logique que
// scenarioDepuisRequete (scenario_contexte.go), mais paramétrable par nom de
// champ puisqu'il en faut deux ici.
func scenarioDepuisChamp(r *http.Request, nom string) *int64 {
	id, err := idOptionnel(r, nom)
	if err != nil || id == nil || *id == 0 {
		return nil
	}
	return id
}

// comparaisonSelecteur est le contexte des deux sélecteurs de scénario de
// l'écran, sur le même patron que selecteurScenarios (scenario_contexte.go)
// mais dédoublé.
type comparaisonSelecteur struct {
	Scenarios   []depot.Scenario
	ScenarioAID int64 // 0 = réel
	ScenarioBID int64
}

// chargerSelecteurComparaison liste les scénarios proposables (BROUILLON et
// ACTIF, comme chargerSelecteurScenarios) et retient ceux choisis pour A et B.
func (s *serveur) chargerSelecteurComparaison(r *http.Request) (comparaisonSelecteur, error) {
	scenarios, err := s.depot.ListerScenarios(false)
	if err != nil {
		return comparaisonSelecteur{}, err
	}
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].Nom < scenarios[j].Nom })

	sel := comparaisonSelecteur{Scenarios: scenarios}
	if id := scenarioDepuisChamp(r, "scenario_a"); id != nil {
		sel.ScenarioAID = *id
	}
	if id := scenarioDepuisChamp(r, "scenario_b"); id != nil {
		sel.ScenarioBID = *id
	}
	return sel, nil
}

// nomScenario renvoie le nom du scénario, ou « Réel » pour nil ou un
// identifiant qui ne résout plus (ne devrait pas arriver derrière un
// sélecteur, mais une lecture ne doit jamais tomber en panne pour ça).
func (s *serveur) nomScenario(id *int64) string {
	if id == nil {
		return "Réel"
	}
	sc, err := s.depot.LireScenario(*id)
	if err != nil {
		return "Réel"
	}
	return sc.Nom
}

// ------------------------------------------------------- page et résultat

func (s *serveur) comparaisonPage(w http.ResponseWriter, r *http.Request) {
	sel, err := s.chargerSelecteurComparaison(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	lignesA, err := vues.ChargerParcCourant(s.depot.Base(), scenarioDepuisChamp(r, "scenario_a"), dateDepuisRequete(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lignesB, err := vues.ChargerParcCourant(s.depot.Base(), scenarioDepuisChamp(r, "scenario_b"), dateDepuisRequete(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	s.rendrePage(w, r, s.titre(r, "titre.comparaison"), "comparaison_page", map[string]any{
		"Date":          dateDepuisRequete(r),
		"Dimensions":    dimensionsAffichables(),
		"Colonnes":      colonnesAffichables(),
		"ValeursFiltre": valeursFiltreParCode(append(append([]vues.Ligne{}, lignesA...), lignesB...)),
		"Scenarios":     sel.Scenarios,
		"ScenarioAID":   sel.ScenarioAID,
		"ScenarioBID":   sel.ScenarioBID,
	})
}

// valeurComparee porte, pour une colonne choisie et un groupe donné (ou le
// total), la valeur sous chaque scénario et leur écart. Un groupe absent
// d'un côté vaut zéro de ce côté — pas une erreur, pas une ligne à part.
type valeurComparee struct {
	A, B, Delta float64
}

// NonNul indique si l'écart mérite d'être mis en évidence dans le gabarit
// (classes ecart-positif / ecart-negatif de besoin_offre.html). Une petite
// tolérance absorbe le bruit de virgule flottante d'une somme, pas une
// différence réelle.
func (v valeurComparee) NonNul() bool {
	return math.Abs(v.Delta) > 1e-9
}

// ligneComparaison est une combinaison de valeurs d'axes (identique des deux
// côtés — c'est la clé de fusion) et les agrégats comparés pour ce groupe.
type ligneComparaison struct {
	Cles    []string
	Valeurs []valeurComparee
}

// resultatComparaison est la forme prête pour le gabarit et les exports.
type resultatComparaison struct {
	LibellesAxes     []string
	LibellesColonnes []string
	NomA, NomB       string
	Lignes           []ligneComparaison
	Total            ligneComparaison
	// NoteGlobal : une colonne de licences est comparée et, d'un côté ou de
	// l'autre, un contrat résolu compte au niveau GLOBAL (non additif —
	// voir resultatAffichable.NoteGlobal, handlers_vue.go).
	NoteGlobal bool
	// Export porte les liens des deux exports dédiés de l'écran
	// (/comparaison/resultat.csv|xlsx avec les paramètres courants), posés
	// sur le tableau de résultat par le fragment via "export_boutons" —
	// l'export universel (tableau.go) n'a pas de second chemin ici.
	Export exportLiens
}

// calculerComparaison lit la configuration du constructeur dans la requête
// (axes, filtres, colonnes — configDepuisRequete, partagé avec /vues) ainsi
// que les deux scénarios A et B, et produit le résultat fusionné.
func (s *serveur) calculerComparaison(r *http.Request) (resultatComparaison, error) {
	axes, filtre, colonnes := configDepuisRequete(r)
	scenarioA := scenarioDepuisChamp(r, "scenario_a")
	scenarioB := scenarioDepuisChamp(r, "scenario_b")
	return s.resultatComparaisonDepuisConfig(axes, filtre, colonnes, scenarioA, scenarioB, dateDepuisRequete(r))
}

// resultatComparaisonDepuisConfig calcule le parc de chaque scénario
// (vues.ChargerParcCourant, qui applique déjà la règle de surcharge —
// modele-donnees.md §6), les groupe séparément (vues.Regrouper), puis fusionne
// par clé de groupe : l'union des clés des deux côtés, triée, un groupe
// absent d'un côté valant zéro de ce côté. Comparer A à lui-même (même
// scénario des deux côtés) est autorisé : chaque écart vaut alors zéro, ce
// n'est pas une erreur.
func (s *serveur) resultatComparaisonDepuisConfig(axes []vues.Dimension, filtre vues.Filtre, colonnes []vues.Colonne, scenarioA, scenarioB *int64, aDate string) (resultatComparaison, error) {
	lignesA, err := vues.ChargerParcCourant(s.depot.Base(), scenarioA, aDate)
	if err != nil {
		return resultatComparaison{}, err
	}
	lignesB, err := vues.ChargerParcCourant(s.depot.Base(), scenarioB, aDate)
	if err != nil {
		return resultatComparaison{}, err
	}
	lignesA = vues.Filtrer(lignesA, filtre)
	lignesB = vues.Filtrer(lignesB, filtre)

	// licences (v3.3) : chaque côté résout ses propres contrats — une
	// surcharge de contrat dans le scénario B est précisément ce qu'on
	// compare. Les axes effectifs sont les mêmes des deux côtés (mêmes axes,
	// mêmes colonnes), on garde ceux de A.
	contratsA, err := s.contratsPourVue(colonnes, scenarioA, aDate)
	if err != nil {
		return resultatComparaison{}, err
	}
	contratsB, err := s.contratsPourVue(colonnes, scenarioB, aDate)
	if err != nil {
		return resultatComparaison{}, err
	}
	groupesA, axesEffectifs := vues.RegrouperAvecLicences(lignesA, axes, colonnes, contratsA)
	groupesB, _ := vues.RegrouperAvecLicences(lignesB, axes, colonnes, contratsB)
	axes = axesEffectifs

	index := map[string]*ligneComparaison{}
	var ordre []string
	fusionner := func(groupes []vues.Groupe, depuisB bool) {
		for _, g := range groupes {
			cle := strings.Join(g.Cles, "\x1f")
			lc, ok := index[cle]
			if !ok {
				lc = &ligneComparaison{Cles: g.Cles, Valeurs: make([]valeurComparee, len(colonnes))}
				index[cle] = lc
				ordre = append(ordre, cle)
			}
			for i, c := range colonnes {
				if depuisB {
					lc.Valeurs[i].B = g.Valeurs[c]
				} else {
					lc.Valeurs[i].A = g.Valeurs[c]
				}
			}
		}
	}
	fusionner(groupesA, false)
	fusionner(groupesB, true)
	sort.Strings(ordre)

	res := resultatComparaison{
		LibellesAxes:     libelles(axes, vues.LibelleDimension),
		LibellesColonnes: libelles(colonnes, vues.LibelleColonne),
		NomA:             s.nomScenario(scenarioA),
		NomB:             s.nomScenario(scenarioB),
		NoteGlobal:       contratGlobalPresent(contratsA) || contratGlobalPresent(contratsB),
	}
	total := make([]valeurComparee, len(colonnes))
	for _, cle := range ordre {
		lc := index[cle]
		for i := range lc.Valeurs {
			lc.Valeurs[i].Delta = lc.Valeurs[i].B - lc.Valeurs[i].A
			total[i].A += lc.Valeurs[i].A
			total[i].B += lc.Valeurs[i].B
		}
		res.Lignes = append(res.Lignes, *lc)
	}
	for i := range total {
		total[i].Delta = total[i].B - total[i].A
	}
	res.Total = ligneComparaison{Valeurs: total}
	return res, nil
}

func (s *serveur) comparaisonResultatFragment(w http.ResponseWriter, r *http.Request) {
	res, err := s.calculerComparaison(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// les boutons d'export du tableau visent les routes dédiées avec la
	// configuration exacte qui a produit ce résultat (la chaîne de requête
	// du hx-get, qui inclut tout le formulaire du constructeur).
	res.Export = exportLiens{
		CSV:  "/comparaison/resultat.csv?" + r.URL.RawQuery,
		XLSX: "/comparaison/resultat.xlsx?" + r.URL.RawQuery,
	}
	s.rendreFragment(w, r, "comparaison_resultat", res)
}

// ----------------------------------------------------------------- export

// enteteComparaison produit les en-têtes attendus par les deux exports :
// les axes, puis pour chaque colonne choisie trois sous-colonnes "<col> A",
// "<col> B", "<col> Δ" — exactement ce que montre le tableau.
func enteteComparaison(res resultatComparaison) []string {
	entetes := append([]string{}, res.LibellesAxes...)
	for _, c := range res.LibellesColonnes {
		entetes = append(entetes, c+" A", c+" B", c+" Δ")
	}
	return entetes
}

// ligneTotaleComparaison construit la ligne « Total » commune aux deux
// exports : les colonnes d'axes portent l'étiquette "Total" dans la première
// (vide dans les suivantes, s'il y en a), puis les agrégats.
func ligneTotaleComparaison(res resultatComparaison) []string {
	ligne := make([]string, 0, len(res.LibellesAxes)+3*len(res.LibellesColonnes))
	for i := range res.LibellesAxes {
		if i == 0 {
			ligne = append(ligne, "Total")
		} else {
			ligne = append(ligne, "")
		}
	}
	for _, v := range res.Total.Valeurs {
		ligne = append(ligne, formaterNombre(v.A), formaterNombre(v.B), formaterNombre(v.Delta))
	}
	return ligne
}

func (s *serveur) comparaisonResultatCSV(w http.ResponseWriter, r *http.Request) {
	res, err := s.calculerComparaison(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="comparaison.csv"`)

	ew := csv.NewWriter(w)
	ew.Comma = ';' // Excel FR ouvre un CSV point-virgule sans ré-import manuel
	_ = ew.Write(enteteComparaison(res))
	for _, l := range res.Lignes {
		ligne := append([]string{}, l.Cles...)
		for _, v := range l.Valeurs {
			ligne = append(ligne, formaterNombre(v.A), formaterNombre(v.B), formaterNombre(v.Delta))
		}
		_ = ew.Write(ligne)
	}
	if len(res.Lignes) > 0 {
		_ = ew.Write(ligneTotaleComparaison(res))
	}
	ew.Flush()
}

func (s *serveur) comparaisonResultatXLSX(w http.ResponseWriter, r *http.Request) {
	res, err := s.calculerComparaison(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	entetes := enteteComparaison(res)
	lignesXLSX := make([][]any, 0, len(res.Lignes)+1)
	for _, l := range res.Lignes {
		ligne := make([]any, 0, len(entetes))
		for _, c := range l.Cles {
			ligne = append(ligne, c)
		}
		for _, v := range l.Valeurs {
			ligne = append(ligne, v.A, v.B, v.Delta)
		}
		lignesXLSX = append(lignesXLSX, ligne)
	}
	if len(res.Lignes) > 0 {
		ligne := make([]any, 0, len(entetes))
		for i := range res.LibellesAxes {
			if i == 0 {
				ligne = append(ligne, "Total")
			} else {
				ligne = append(ligne, "")
			}
		}
		for _, v := range res.Total.Valeurs {
			ligne = append(ligne, v.A, v.B, v.Delta)
		}
		lignesXLSX = append(lignesXLSX, ligne)
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="comparaison.xlsx"`)
	if err := xlsx.Ecrire(w, xlsx.Feuille{Nom: "Comparaison", Entetes: entetes, Lignes: lignesXLSX}); err != nil {
		// des octets sont peut-être déjà partis (l'archive zip s'écrit en
		// flux) : on ne peut plus changer le code de statut, seulement
		// journaliser.
		s.erreurServeur(w, r, err)
	}
}
