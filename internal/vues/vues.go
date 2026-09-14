// Package vues est le constructeur de vues (v1.6 du backlog) : le
// remplaçant des tableaux croisés dynamiques de l'Excel actuel. Axes de
// regroupement libres, filtres multiples, colonnes agrégées.
//
// Portée : l'état du parc à une date — le jour même dans 95 % des cas, une
// date passée pour relire le parc tel qu'il était (backlog v3.0 : la vue
// parc est une vue d'assets, la consommation de licences se lit sur le parc
// actif à un instant). ChargerParcCourant accepte aussi un scénario (backlog
// v2.1 : « toutes les vues et tous les calculs paramétrables par scénario »).
//
// Le regroupement se fait en mémoire, pas en SQL dynamique : à l'échelle du
// projet (~2000 serveurs, docs/modele-donnees.md §0) c'est instantané, et ça
// élimine tout risque d'injection par construction de GROUP BY à partir d'une
// entrée utilisateur.
package vues

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"parallax/internal/depot"
)

// Dimension est un axe de regroupement ou de filtre.
type Dimension string

const (
	DimProjet        Dimension = "projet"
	DimEnvironnement Dimension = "environnement"
	DimTechno        Dimension = "techno"
	DimTier          Dimension = "tier"
	DimUsage         Dimension = "usage"
	DimCluster       Dimension = "cluster"
	DimZone          Dimension = "zone"
	DimModele        Dimension = "modele"
	DimAnneeModele   Dimension = "annee_modele" // génération : l'année de la commande annuelle, tous types confondus
	DimStatutServeur Dimension = "statut"
)

// Dimensions liste toutes les dimensions disponibles, dans l'ordre
// d'affichage proposé à l'utilisateur.
var Dimensions = []Dimension{
	DimProjet, DimEnvironnement, DimTechno, DimTier, DimUsage,
	DimCluster, DimZone, DimModele, DimAnneeModele, DimStatutServeur,
}

// LibelleDimension renvoie le libellé français d'une dimension, pour les
// gabarits.
func LibelleDimension(d Dimension) string {
	switch d {
	case DimProjet:
		return "Projet"
	case DimEnvironnement:
		return "Environnement"
	case DimTechno:
		return "Techno"
	case DimTier:
		return "Tier"
	case DimUsage:
		return "Usage"
	case DimCluster:
		return "Cluster"
	case DimZone:
		return "Zone"
	case DimModele:
		return "Modèle"
	case DimAnneeModele:
		return "Année du modèle"
	case DimStatutServeur:
		return "Statut serveur"
	default:
		return string(d)
	}
}

// Ligne est l'état courant d'un serveur affecté dans le réel : une ligne par
// serveur, avec ses dimensions et ses mesures numériques déjà résolues.
type Ligne struct {
	ServeurID int64
	Statut    string
	// TechnoID et ClusterID sont les identifiants derrière TechnoCode et
	// ClusterNom : les colonnes de licences (licences.go) s'appuient dessus
	// — un contrat est indexé par techno, un niveau CLUSTER regroupe par
	// cluster — et un code n'est pas garanti stable là où un identifiant l'est.
	TechnoID          int64
	ClusterID         int64
	ProjetCode        string
	EnvironnementCode string
	TechnoCode        string
	TierCode          string
	UsageCode         string
	ClusterNom        string
	ZoneCode          string
	ModeleCode        string
	AnneeModele       string // "2025" ; vide sans révision rattachée

	// Quantites et Capacites sont indexées par code de composant de la
	// révision rattachée (depot.ComposantsCanoniques, mais tout code présent
	// en base y figure) : la quantité seule (nombre de disques, de cartes,
	// de GPU) et le total quantité × capacité unitaire (To, Gbps, cœurs…).
	// Un serveur sans révision a des cartes vides.
	Quantites map[string]float64
	Capacites map[string]float64

	NbNoeuds        float64
	PrixFournisseur float64
	CoutAnnuel      float64
}

// Quantite renvoie la quantité cumulée du composant de code donné (0 si absent).
func (l Ligne) Quantite(code string) float64 { return l.Quantites[code] }

// Capacite renvoie la capacité totale du composant de code donné (0 si absent).
func (l Ligne) Capacite(code string) float64 { return l.Capacites[code] }

// Valeur renvoie la valeur affichable d'une ligne pour une dimension donnée.
// Une chaîne vide signifie « non renseigné » (tier ou usage absent, serveur
// sans zone ou sans révision rattachée…).
func (l Ligne) Valeur(d Dimension) string {
	switch d {
	case DimProjet:
		return l.ProjetCode
	case DimEnvironnement:
		return l.EnvironnementCode
	case DimTechno:
		return l.TechnoCode
	case DimTier:
		return l.TierCode
	case DimUsage:
		return l.UsageCode
	case DimCluster:
		return l.ClusterNom
	case DimZone:
		return l.ZoneCode
	case DimModele:
		return l.ModeleCode
	case DimAnneeModele:
		return l.AnneeModele
	case DimStatutServeur:
		return l.Statut
	default:
		return ""
	}
}

const sqlParcCourant = `
SELECT
    s.id, s.statut, c.techno_id, c.id,
    COALESCE(pr.code, ''), COALESCE(env.code, ''), COALESCE(tech.code, ''),
    COALESCE(tier.code, ''), COALESCE(usg.code, ''), COALESCE(c.nom, ''),
    COALESCE(dc.code, ''), COALESCE(m.code, ''), COALESCE(m.annee, 0),
    COALESCE(mn.nb_noeuds, 0),
    COALESCE(m.prix_fournisseur_ht, 0), COALESCE(m.cout_annuel_ht, 0)
FROM serveur s
JOIN affectation a ON a.serveur_id = s.id
    AND a.date_debut <= ?1 AND (a.date_fin IS NULL OR a.date_fin >= ?1) AND %s
JOIN cluster c ON c.id = a.cluster_id
LEFT JOIN projet pr             ON pr.id = c.projet_id
LEFT JOIN environnement env     ON env.id = c.environnement_id
LEFT JOIN techno tech           ON tech.id = c.techno_id
LEFT JOIN tier                  ON tier.id = c.tier_id
LEFT JOIN usage_fonctionnel usg ON usg.id = c.usage_fonctionnel_id
LEFT JOIN zone dc         ON dc.id = s.zone_id
LEFT JOIN serveur_revision sr   ON sr.serveur_id = s.id
    AND sr.date_debut <= ?1 AND (sr.date_fin IS NULL OR sr.date_fin >= ?1)
LEFT JOIN revision rev          ON rev.id = sr.revision_id
LEFT JOIN modele m              ON m.id = rev.modele_id
LEFT JOIN modele_noeud mn       ON mn.revision_id = rev.id AND mn.techno_id = c.techno_id
WHERE %s
ORDER BY s.id`

// sqlComposantsCourants : pour chaque serveur, par code de composant de sa
// révision à la date, la quantité cumulée et la capacité totale. Chargé à
// part de sqlParcCourant (une ligne par serveur) pour ne pas figer la liste
// des codes dans la requête : ajouter un composant canonique n'y change rien.
const sqlComposantsCourants = `
SELECT sr.serveur_id, comp.code,
       SUM(comp.quantite), SUM(comp.quantite * comp.capacite_unitaire)
FROM serveur_revision sr
JOIN composant comp ON comp.revision_id = sr.revision_id
WHERE sr.date_debut <= ?1 AND (sr.date_fin IS NULL OR sr.date_fin >= ?1)
GROUP BY sr.serveur_id, comp.code`

// ChargerParcCourant interroge la base et renvoie une ligne par serveur
// affecté à un cluster à la date aDate (YYYY-MM-DD, typiquement le jour
// même), avec ses dimensions et mesures — révision rattachée à cette date.
//
// scenarioID nil renvoie le parc réel. Une valeur applique la même règle de
// surcharge que depot.AffectationsResolues (voir modele-donnees.md §6) : un
// serveur touché par le scénario — déplacé, retiré, ou ajouté à l'état
// d'hypothèse — n'apparaît que sous sa forme du scénario, jamais en double
// avec sa forme réelle. Un serveur non touché par le scénario apparaît sous
// sa forme réelle, inchangée.
//
// Un serveur non affecté (statut HYPOTHESE mis à part, hors réel ou hors
// scénario) n'apparaît pas : la vue porte sur le parc en service, comme les
// tableaux croisés actuels.
func ChargerParcCourant(base *sql.DB, scenarioID *int64, aDate string) ([]Ligne, error) {
	if err := depot.ValiderDate("date", aDate); err != nil {
		return nil, err
	}
	reel, err := chargerLignesSeau(base, nil, aDate)
	if err != nil {
		return nil, err
	}
	if scenarioID == nil {
		return trierParServeur(reel), nil
	}

	duScenario, err := chargerLignesSeau(base, scenarioID, aDate)
	if err != nil {
		return nil, err
	}
	touches, err := serveursTouchesParScenario(base, *scenarioID)
	if err != nil {
		return nil, err
	}

	fusion := make(map[int64]Ligne, len(reel)+len(duScenario))
	for id, l := range reel {
		if _, touche := touches[id]; touche {
			continue // la surcharge du scénario l'emporte entièrement — voir §6
		}
		fusion[id] = l
	}
	for id, l := range duScenario {
		fusion[id] = l
	}
	return trierParServeur(fusion), nil
}

// chargerLignesSeau exécute sqlParcCourant sur le seau désigné par scenarioID
// (nil = réel) et renvoie les lignes indexées par identifiant de serveur.
//
// Deux filtres distincts, à ne pas confondre : celui de l'affectation (quel
// seau d'affectation on lit) et celui du serveur lui-même (s.scenario_id
// n'est renseigné que pour un serveur HYPOTHESE — invariant 5 — et doit
// laisser passer les serveurs réels même en lecture sous scénario, faute de
// quoi un serveur réel déplacé par le scénario disparaîtrait entièrement :
// son affectation serait bien trouvée dans le seau du scénario, mais la
// ligne serveur elle-même resterait exclue par ce second filtre).
func chargerLignesSeau(base *sql.DB, scenarioID *int64, aDate string) (map[int64]Ligne, error) {
	// paramètres numérotés : ?1 la date (répétée dans la requête), ?2 le
	// scénario — l'ordre des « ? » anonymes deviendrait fragile avec la date
	// répétée quatre fois.
	filtreAffectation, filtreServeur, args := "a.scenario_id IS NULL", "s.scenario_id IS NULL", []any{aDate}
	if scenarioID != nil {
		filtreAffectation = "a.scenario_id = ?2"
		filtreServeur = "(s.scenario_id IS NULL OR s.scenario_id = ?2)"
		args = []any{aDate, *scenarioID}
	}
	lignes, err := base.Query(fmt.Sprintf(sqlParcCourant, filtreAffectation, filtreServeur), args...)
	if err != nil {
		return nil, fmt.Errorf("chargement du parc courant : %w", err)
	}
	defer lignes.Close()

	out := map[int64]Ligne{}
	for lignes.Next() {
		l := Ligne{Quantites: map[string]float64{}, Capacites: map[string]float64{}}
		var anneeModele int64
		if err := lignes.Scan(&l.ServeurID, &l.Statut, &l.TechnoID, &l.ClusterID, &l.ProjetCode, &l.EnvironnementCode,
			&l.TechnoCode, &l.TierCode, &l.UsageCode, &l.ClusterNom, &l.ZoneCode,
			&l.ModeleCode, &anneeModele, &l.NbNoeuds, &l.PrixFournisseur, &l.CoutAnnuel); err != nil {
			return nil, fmt.Errorf("chargement du parc courant : %w", err)
		}
		if anneeModele > 0 {
			l.AnneeModele = strconv.FormatInt(anneeModele, 10)
		}
		out[l.ServeurID] = l
	}
	if err := lignes.Err(); err != nil {
		return nil, err
	}
	return out, chargerComposants(base, out, aDate)
}

// chargerComposants remplit Quantites et Capacites des lignes à partir de la
// révision courante de chaque serveur. Les serveurs absents de lignes (non
// affectés dans ce seau) sont ignorés.
func chargerComposants(base *sql.DB, lignes map[int64]Ligne, aDate string) error {
	rows, err := base.Query(sqlComposantsCourants, aDate)
	if err != nil {
		return fmt.Errorf("chargement des composants du parc : %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			serveurID int64
			code      string
			quantite  float64
			capacite  float64
		)
		if err := rows.Scan(&serveurID, &code, &quantite, &capacite); err != nil {
			return fmt.Errorf("chargement des composants du parc : %w", err)
		}
		l, ok := lignes[serveurID]
		if !ok {
			continue
		}
		l.Quantites[code] = quantite
		l.Capacites[code] = capacite
	}
	return rows.Err()
}

// serveursTouchesParScenario renvoie l'ensemble des serveurs qui ont au moins
// une ligne d'affectation dans le seau du scénario — actif ou non aujourd'hui.
// C'est ce qui détermine si la surcharge s'applique à ce serveur, exactement
// comme dans depot.AffectationsResolues.
func serveursTouchesParScenario(base *sql.DB, scenarioID int64) (map[int64]struct{}, error) {
	lignes, err := base.Query(
		`SELECT DISTINCT serveur_id FROM affectation WHERE scenario_id = ?`, scenarioID)
	if err != nil {
		return nil, fmt.Errorf("serveurs touchés par le scénario : %w", err)
	}
	defer lignes.Close()

	out := map[int64]struct{}{}
	for lignes.Next() {
		var id int64
		if err := lignes.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, lignes.Err()
}

func trierParServeur(m map[int64]Ligne) []Ligne {
	out := make([]Ligne, 0, len(m))
	for _, l := range m {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServeurID < out[j].ServeurID })
	return out
}

// Filtre restreint les lignes dont la valeur d'une dimension appartient à
// l'ensemble choisi. Une dimension absente de la carte, ou associée à un
// ensemble vide, ne restreint rien.
type Filtre map[Dimension][]string

// Filtrer applique f à lignes.
func Filtrer(lignes []Ligne, f Filtre) []Ligne {
	if len(f) == 0 {
		return lignes
	}
	var out []Ligne
	for _, l := range lignes {
		if matche(l, f) {
			out = append(out, l)
		}
	}
	return out
}

func matche(l Ligne, f Filtre) bool {
	for dim, valeurs := range f {
		if len(valeurs) == 0 {
			continue
		}
		v := l.Valeur(dim)
		trouve := false
		for _, autorisee := range valeurs {
			if v == autorisee {
				trouve = true
				break
			}
		}
		if !trouve {
			return false
		}
	}
	return true
}

// ValeursDisponibles liste, pour chaque dimension, les valeurs distinctes
// présentes dans lignes, triées — sert à construire les listes de filtre
// dans l'IHM.
func ValeursDisponibles(lignes []Ligne) map[Dimension][]string {
	ensembles := map[Dimension]map[string]struct{}{}
	for _, d := range Dimensions {
		ensembles[d] = map[string]struct{}{}
	}
	for _, l := range lignes {
		for _, d := range Dimensions {
			if v := l.Valeur(d); v != "" {
				ensembles[d][v] = struct{}{}
			}
		}
	}
	out := map[Dimension][]string{}
	for _, d := range Dimensions {
		vals := make([]string, 0, len(ensembles[d]))
		for v := range ensembles[d] {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		out[d] = vals
	}
	return out
}

// Colonne est un agrégat calculable sur un groupe de lignes.
//
// Trois familles : les colonnes fixes (nombre de serveurs, nœuds, coûts),
// la capacité totale d'un composant canonique (identifiant = son code :
// « cpu », « ssd », « gpu_ram »…) et, pour un composant dénombré, sa quantité
// seule (identifiant = code + « _nb » : « ssd_nb », « nic_nb »). Les
// identifiants sont persistés dans les vues enregistrées : ne pas les
// renommer sans migration.
type Colonne string

const (
	ColNbServeurs      Colonne = "nb_serveurs"
	ColNbNoeuds        Colonne = "nb_noeuds"
	ColCoutAcquisition Colonne = "cout_acquisition"
	ColCoutAnnuel      Colonne = "cout_annuel"

	// Colonnes de licences (v3.3) : calculées sur le groupe, pas par somme
	// de lignes — voir licences.go et RegrouperAvecLicences. Regrouper seul
	// les laisse à 0.
	ColLicencesUnites Colonne = "licences_unites"
	ColLicencesCout   Colonne = "licences_cout"
)

// colonneComposant relie une colonne à un composant canonique : Nb vrai
// agrège la quantité, faux la capacité totale.
type colonneComposant struct {
	Code    string
	Nb      bool
	Libelle string
}

// colonnesComposants est dérivée de depot.ComposantsCanoniques, dans son
// ordre : quantité (si elle a un sens) puis capacité totale de chaque code.
var colonnesComposants = func() map[Colonne]colonneComposant {
	out := map[Colonne]colonneComposant{}
	for _, c := range depot.ComposantsCanoniques {
		if c.LibelleNb != "" {
			out[Colonne(c.Code+"_nb")] = colonneComposant{Code: c.Code, Nb: true, Libelle: c.LibelleNb}
		}
		out[Colonne(c.Code)] = colonneComposant{Code: c.Code, Libelle: c.Libelle}
	}
	return out
}()

// Colonnes liste tous les agrégats disponibles, dans l'ordre d'affichage.
var Colonnes = func() []Colonne {
	out := []Colonne{ColNbServeurs}
	for _, c := range depot.ComposantsCanoniques {
		if c.LibelleNb != "" {
			out = append(out, Colonne(c.Code+"_nb"))
		}
		out = append(out, Colonne(c.Code))
	}
	return append(out, ColNbNoeuds, ColCoutAcquisition, ColCoutAnnuel, ColLicencesUnites, ColLicencesCout)
}()

// LibelleColonne renvoie le libellé français d'un agrégat.
func LibelleColonne(c Colonne) string {
	switch c {
	case ColNbServeurs:
		return "Nb serveurs"
	case ColNbNoeuds:
		return "Nœuds"
	case ColCoutAcquisition:
		return "Coût d'acquisition (HT)"
	case ColCoutAnnuel:
		return "Coût annuel (HT)"
	case ColLicencesUnites:
		return "Unités de licence"
	case ColLicencesCout:
		return "Coût licences (HT)"
	}
	if cc, ok := colonnesComposants[c]; ok {
		return cc.Libelle
	}
	return string(c)
}

func valeurColonne(l Ligne, c Colonne) float64 {
	switch c {
	case ColNbServeurs:
		return 1
	case ColNbNoeuds:
		return l.NbNoeuds
	case ColCoutAcquisition:
		return l.PrixFournisseur
	case ColCoutAnnuel:
		return l.CoutAnnuel
	}
	if cc, ok := colonnesComposants[c]; ok {
		if cc.Nb {
			return l.Quantites[cc.Code]
		}
		return l.Capacites[cc.Code]
	}
	return 0
}

// Groupe est une combinaison de valeurs d'axes et les agrégats demandés pour
// les lignes qui y correspondent.
type Groupe struct {
	Cles    []string
	Valeurs map[Colonne]float64
}

// Regrouper construit le tableau croisé : une ligne de résultat par
// combinaison distincte des valeurs des axes (dans l'ordre fourni — c'est
// l'ordre hiérarchique choisi par l'utilisateur), avec les agrégats calculés
// sur les lignes de ce groupe. Les groupes sont triés par leurs clés.
//
// Sans axe (axes vide), un unique groupe agrège toutes les lignes — c'est le
// total général. Les colonnes de licences valent 0 ici : elles demandent
// les contrats, voir RegrouperAvecLicences.
func Regrouper(lignes []Ligne, axes []Dimension, colonnes []Colonne) []Groupe {
	return regrouper(lignes, axes, colonnes, nil)
}

// partition est un groupe avant agrégation : ses clés et les lignes qui y
// tombent — nécessaire aux colonnes calculées sur le groupe entier (licences)
// et non par somme de lignes.
type partition struct {
	cles   []string
	lignes []Ligne
}

// partitionner découpe lignes par combinaison de valeurs d'axes, dans
// l'ordre trié des clés (le même tri que celui du résultat).
func partitionner(lignes []Ligne, axes []Dimension) []partition {
	index := map[string]*partition{}
	var ordre []string
	for _, l := range lignes {
		cles := make([]string, len(axes))
		for i, axe := range axes {
			cles[i] = l.Valeur(axe)
		}
		cle := strings.Join(cles, "\x1f")
		p, ok := index[cle]
		if !ok {
			p = &partition{cles: cles}
			index[cle] = p
			ordre = append(ordre, cle)
		}
		p.lignes = append(p.lignes, l)
	}
	sort.Strings(ordre)
	out := make([]partition, 0, len(ordre))
	for _, cle := range ordre {
		out = append(out, *index[cle])
	}
	return out
}

// regrouper somme chaque colonne par groupe, sauf les colonnes de licences,
// calculées sur le groupe via CalculerLicences quand contrats est fourni
// (sinon 0).
func regrouper(lignes []Ligne, axes []Dimension, colonnes []Colonne, contrats map[int64]depot.LicenceContrat) []Groupe {
	avecLicences := DemandeLicences(colonnes)
	out := make([]Groupe, 0)
	for _, p := range partitionner(lignes, axes) {
		g := Groupe{Cles: p.cles, Valeurs: map[Colonne]float64{}}
		for _, col := range colonnes {
			g.Valeurs[col] = 0
			for _, l := range p.lignes {
				g.Valeurs[col] += valeurColonne(l, col)
			}
		}
		if avecLicences && contrats != nil {
			unites, cout := CalculerLicences(p.lignes, contrats)
			for _, col := range colonnes {
				switch col {
				case ColLicencesUnites:
					g.Valeurs[col] = unites
				case ColLicencesCout:
					g.Valeurs[col] = cout
				}
			}
		}
		out = append(out, g)
	}
	return out
}
