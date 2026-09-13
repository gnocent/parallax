package capacity

import (
	"fmt"
	"math"
	"sort"
)

// ============================================================ types

// Cluster est le périmètre de calcul : l'intersection concrète des dimensions.
type Cluster struct {
	ID              int64  `json:"id"`
	ProjetID        *int64 `json:"projet_id"`
	EnvironnementID *int64 `json:"environnement_id"`
	TechnoID        *int64 `json:"techno_id"`
	TierID          *int64 `json:"tier_id"`
	UsageID         *int64 `json:"usage_fonctionnel_id"`
}

// ValeurVariable est une valeur saisie, avec sa portée et son contexte.
// Une colonne de portée nulle signifie « ne restreint pas ».
type ValeurVariable struct {
	Code            string  `json:"code"`
	Annee           int     `json:"annee"`
	ScenarioID      *int64  `json:"scenario_id"`
	ProjetID        *int64  `json:"projet_id"`
	EnvironnementID *int64  `json:"environnement_id"`
	TechnoID        *int64  `json:"techno_id"`
	TierID          *int64  `json:"tier_id"`
	ClusterID       *int64  `json:"cluster_id"`
	Valeur          float64 `json:"valeur"`
}

// Regle produit un besoin dans une métrique, sur le domaine défini par son filtre.
type Regle struct {
	ID               int64  `json:"id"`
	Nom              string `json:"nom"`
	Metrique         string `json:"metrique"`
	Expression       string `json:"expression"`
	NiveauEvaluation string `json:"niveau_evaluation"`
	ComposantOffre   string `json:"composant_offre"`
	Actif            bool   `json:"actif"`

	ProjetID        *int64 `json:"projet_id"`
	EnvironnementID *int64 `json:"environnement_id"`
	TechnoID        *int64 `json:"techno_id"`
	TierID          *int64 `json:"tier_id"`
	UsageID         *int64 `json:"usage_fonctionnel_id"`
	ClusterID       *int64 `json:"cluster_id"`
}

const (
	NiveauPerimetre  = "PERIMETRE"
	NiveauParServeur = "PAR_SERVEUR"
)

// Modele est un candidat matériel : ses composants fournissent l'offre.
type Modele struct {
	Code              string             `json:"code"`
	PrixFournisseurHT float64            `json:"prix_fournisseur_ht"`
	CoutAnnuelHT      float64            `json:"cout_annuel_ht"`
	Composants        map[string]float64 `json:"composants"`
}

// Types de contraintes de dimensionnement, portées par le cluster.
const (
	ContrainteMinTotal        = "MIN_TOTAL"
	ContrainteMinParZone      = "MIN_PAR_ZONE"
	ContrainteMultipleTotal   = "MULTIPLE_TOTAL"
	ContrainteMultipleParZone = "MULTIPLE_PAR_ZONE"
	ContrainteNbZones         = "NB_ZONES"
	ContrainteEquilibrageZone = "EQUILIBRAGE_ZONE"
)

// Contraintes associe un type de contrainte à sa valeur. EQUILIBRAGE_ZONE est
// un drapeau : toute valeur non nulle l'active.
type Contraintes map[string]float64

// Repartition est le résultat de l'application des contraintes.
type Repartition struct {
	NbServeurs int  `json:"nb_serveurs"`
	ParZone    *int `json:"par_zone"`
	NbZones    *int `json:"nb_zones"`
}

// DetailRegle documente la contribution d'une règle au dimensionnement.
// BesoinResiduel est ce qui reste à couvrir par le modèle candidat une fois
// déduite l'offre conservée (égal à Besoin en renouvellement complet).
type DetailRegle struct {
	RegleID          int64   `json:"regle_id"`
	Nom              string  `json:"nom"`
	Metrique         string  `json:"metrique"`
	Besoin           float64 `json:"besoin"`
	BesoinResiduel   float64 `json:"besoin_residuel"`
	CapaciteUnitaire float64 `json:"capacite_unitaire"`
	NbBrut           float64 `json:"nb_brut"`
}

// Dimensionnement est le résultat complet pour un cluster et un modèle candidat.
type Dimensionnement struct {
	NbServeurs      int           `json:"nb_serveurs"`
	ParZone         *int          `json:"par_zone"`
	NbZones         *int          `json:"nb_zones"`
	RegleLimitante  *int64        `json:"regle_limitante"`
	Details         []DetailRegle `json:"details"`
	CoutAcquisition float64       `json:"cout_acquisition"`
	CoutAnnuel      float64       `json:"cout_annuel"`
}

// Conflit signale deux règles d'une même métrique qui matchent un cluster commun.
type Conflit struct {
	RegleA   int64   `json:"regle_a"`
	RegleB   int64   `json:"regle_b"`
	Clusters []int64 `json:"clusters"`
}

// ============================================== résolution des variables

// Poids de spécificité des portées, du plus général au plus spécifique.
// Puissances de deux : une portée fine l'emporte toujours sur n'importe
// quelle combinaison de portées plus grossières.
const (
	poidsProjet        = 1 << 0
	poidsEnvironnement = 1 << 1
	poidsTechno        = 1 << 2
	poidsTier          = 1 << 3
	poidsCluster       = 1 << 4
)

func memeID(attendu, valeur *int64) bool {
	if attendu == nil {
		return true // la portée ne restreint pas
	}
	return valeur != nil && *valeur == *attendu
}

// ResoudreVariable retourne la valeur applicable à un cluster pour une année
// et un scénario donnés.
//
// Priorité : le scénario avant le réel. À égalité, la portée la plus
// spécifique l'emporte. Si rien ne s'applique, le second retour est false et
// l'appelant utilise la valeur par défaut de la variable.
func ResoudreVariable(valeurs []ValeurVariable, code string, c Cluster,
	annee int, scenarioID *int64) (float64, bool) {

	meilleure := 0.0
	meilleurPoids := -1
	meilleurScenario := -1

	for _, v := range valeurs {
		if v.Code != code || v.Annee != annee {
			continue
		}
		if v.ScenarioID != nil {
			if scenarioID == nil || *v.ScenarioID != *scenarioID {
				continue
			}
		}

		poids := 0
		applicable := true
		type portee struct {
			attendu *int64
			valeur  *int64
			poids   int
		}
		idCluster := c.ID
		for _, p := range []portee{
			{v.ProjetID, c.ProjetID, poidsProjet},
			{v.EnvironnementID, c.EnvironnementID, poidsEnvironnement},
			{v.TechnoID, c.TechnoID, poidsTechno},
			{v.TierID, c.TierID, poidsTier},
			{v.ClusterID, &idCluster, poidsCluster},
		} {
			if p.attendu == nil {
				continue
			}
			if !memeID(p.attendu, p.valeur) {
				applicable = false
				break
			}
			poids += p.poids
		}
		if !applicable {
			continue
		}

		prio := 0
		if v.ScenarioID != nil {
			prio = 1
		}
		if prio > meilleurScenario || (prio == meilleurScenario && poids > meilleurPoids) {
			meilleurScenario, meilleurPoids, meilleure = prio, poids, v.Valeur
		}
	}

	if meilleurPoids < 0 {
		return 0, false
	}
	return meilleure, true
}

// ============================================ matching et chevauchement

// RegleMatche indique si une règle s'applique à un cluster : chacun de ses
// critères doit être vide ou égal à la valeur du cluster.
func RegleMatche(r Regle, c Cluster) bool {
	idCluster := c.ID
	paires := [][2]*int64{
		{r.ProjetID, c.ProjetID},
		{r.EnvironnementID, c.EnvironnementID},
		{r.TechnoID, c.TechnoID},
		{r.TierID, c.TierID},
		{r.UsageID, c.UsageID},
		{r.ClusterID, &idCluster},
	}
	for _, p := range paires {
		if !memeID(p[0], p[1]) {
			return false
		}
	}
	return true
}

// DetecterConflits liste les paires de règles actives d'une même métrique qui
// s'appliquent toutes deux à au moins un cluster existant.
//
// On raisonne sur les clusters réels et non sur des combinaisons théoriques :
// c'est plus simple, et le message d'erreur peut nommer les clusters concernés.
func DetecterConflits(regles []Regle, clusters []Cluster) []Conflit {
	var actives []Regle
	for _, r := range regles {
		if r.Actif {
			actives = append(actives, r)
		}
	}

	var conflits []Conflit
	for i := range actives {
		for j := i + 1; j < len(actives); j++ {
			a, b := actives[i], actives[j]
			if a.Metrique != b.Metrique {
				continue
			}
			var communs []int64
			for _, c := range clusters {
				if RegleMatche(a, c) && RegleMatche(b, c) {
					communs = append(communs, c.ID)
				}
			}
			if len(communs) > 0 {
				sort.Slice(communs, func(x, y int) bool { return communs[x] < communs[y] })
				conflits = append(conflits, Conflit{RegleA: a.ID, RegleB: b.ID, Clusters: communs})
			}
		}
	}
	return conflits
}

// ==================================================== contraintes

func multipleSuperieur(n int, m int) int {
	if m <= 0 {
		return n
	}
	return int(math.Ceil(float64(n)/float64(m))) * m
}

// AppliquerContraintes convertit un nombre de serveurs brut en nombre entier
// respectant les contraintes du cluster.
//
// Ordre : arrondi supérieur, minimum total, multiple total, puis contraintes
// par zone. Contraintes totales et par zone pouvant se contredire,
// on itère jusqu'au point fixe.
func AppliquerContraintes(nbBrut float64, c Contraintes) Repartition {
	valeur := func(cle string) int {
		v, ok := c[cle]
		if !ok {
			return 0
		}
		return int(v)
	}

	nbZone := valeur(ContrainteNbZones)
	minTotal := valeur(ContrainteMinTotal)
	multTotal := valeur(ContrainteMultipleTotal)
	minZone := valeur(ContrainteMinParZone)
	multZone := valeur(ContrainteMultipleParZone)
	equilibrage := c[ContrainteEquilibrageZone] != 0

	// la tolérance absorbe les erreurs de représentation flottante :
	// 12.000000000001 ne doit pas donner 13 serveurs
	nb := int(math.Ceil(nbBrut - 1e-9))
	repartitionForcee := nbZone > 0 && (minZone > 0 || multZone > 0 || equilibrage)

	var parZone *int
	for iteration := 0; iteration < 100; iteration++ {
		precedent := nb
		if minTotal > 0 && nb < minTotal {
			nb = minTotal
		}
		if multTotal > 0 {
			nb = multipleSuperieur(nb, multTotal)
		}
		if repartitionForcee {
			p := int(math.Ceil(float64(nb) / float64(nbZone)))
			if minZone > 0 && p < minZone {
				p = minZone
			}
			if multZone > 0 {
				p = multipleSuperieur(p, multZone)
			}
			parZone = &p
			nb = p * nbZone
		}
		if nb == precedent {
			break
		}
	}

	r := Repartition{NbServeurs: nb, ParZone: parZone}
	if nbZone > 0 {
		n := nbZone
		r.NbZones = &n
	}
	return r
}

// ==================================================== dimensionnement

// EntreeDimensionnement rassemble tout ce qu'il faut pour dimensionner un
// cluster pour un modèle candidat.
type EntreeDimensionnement struct {
	Cluster     Cluster
	Regles      []Regle
	Valeurs     []ValeurVariable
	Defauts     map[string]float64
	Annee       int
	ScenarioID  *int64
	Modele      Modele
	Contraintes Contraintes

	// OffreConservee (v2.3, renfort) : capacité par code de composant des
	// serveurs déjà installés que l'hypothèse conserve — une sélection choisie
	// par l'utilisateur, pas forcément tout le parc. Déduite du besoin de
	// chaque règle avant division par la capacité du modèle candidat. Vide :
	// renouvellement complet.
	OffreConservee map[string]float64

	// Agregats fournit les valeurs déjà calculées sur le périmètre
	// (ram_totale, nb_serveurs...), disponibles dans les formules.
	Agregats map[string]float64

	// Serveurs alimente les règles de niveau PAR_SERVEUR : une entrée par
	// serveur du périmètre, avec ses attributs (ram_machine...).
	Serveurs []map[string]float64
}

// BesoinRegle est le besoin brut d'une règle applicable, avant toute
// confrontation à un modèle candidat.
type BesoinRegle struct {
	RegleID        int64
	Nom            string
	Metrique       string
	ComposantOffre string
	Besoin         float64
}

// CalculerBesoins évalue le besoin de chaque règle active applicable au
// cluster (résolution des variables puis évaluation, au périmètre ou par
// serveur puis somme). Ne rapporte à aucune capacité de modèle : c'est la
// brique commune à Dimensionner, qui divise ensuite par la capacité d'un
// modèle candidat, et à l'écran besoin / offre / écart, qui compare ce même
// besoin à l'offre déjà installée, indépendamment de tout modèle candidat.
func CalculerBesoins(e EntreeDimensionnement) ([]BesoinRegle, error) {
	var applicables []Regle
	for _, r := range e.Regles {
		if r.Actif && RegleMatche(r, e.Cluster) {
			applicables = append(applicables, r)
		}
	}
	sort.Slice(applicables, func(i, j int) bool { return applicables[i].ID < applicables[j].ID })

	besoins := make([]BesoinRegle, 0, len(applicables))
	for _, r := range applicables {
		expression, err := Compiler(r.Expression)
		if err != nil {
			return nil, fmt.Errorf("règle %d (%s) : %w", r.ID, r.Nom, err)
		}

		env := map[string]float64{}
		for k, v := range e.Agregats {
			env[k] = v
		}
		for _, code := range expression.Variables() {
			if _, deja := env[code]; deja {
				continue
			}
			if v, ok := ResoudreVariable(e.Valeurs, code, e.Cluster, e.Annee, e.ScenarioID); ok {
				env[code] = v
				continue
			}
			if v, ok := e.Defauts[code]; ok {
				env[code] = v
				continue
			}
			return nil, fmt.Errorf(
				"règle %d (%s) : variable « %s » non résolue", r.ID, r.Nom, code)
		}

		var besoin float64
		if r.NiveauEvaluation == NiveauParServeur {
			for _, serveur := range e.Serveurs {
				local := make(map[string]float64, len(env)+len(serveur))
				for k, v := range env {
					local[k] = v
				}
				for k, v := range serveur {
					local[k] = v
				}
				v, err := expression.Evaluer(local)
				if err != nil {
					return nil, fmt.Errorf("règle %d (%s) : %w", r.ID, r.Nom, err)
				}
				besoin += v
			}
		} else {
			v, err := expression.Evaluer(env)
			if err != nil {
				return nil, fmt.Errorf("règle %d (%s) : %w", r.ID, r.Nom, err)
			}
			besoin = v
		}

		besoins = append(besoins, BesoinRegle{
			RegleID: r.ID, Nom: r.Nom, Metrique: r.Metrique,
			ComposantOffre: r.ComposantOffre, Besoin: besoin,
		})
	}
	return besoins, nil
}

// Dimensionner calcule le nombre de serveurs nécessaires.
//
// Ordre imposé, à ne pas réorganiser :
//  1. besoin brut par règle applicable
//  2. nombre de serveurs brut = besoin / capacité unitaire du modèle
//  3. maximum sur les règles : la gagnante est le facteur limitant
//  4. contraintes du cluster
//
// Arrondir avant le maximum donnerait un résultat différent et faux.
func Dimensionner(e EntreeDimensionnement) (Dimensionnement, error) {
	besoins, err := CalculerBesoins(e)
	if err != nil {
		return Dimensionnement{}, err
	}

	details := make([]DetailRegle, 0, len(besoins))
	for _, b := range besoins {
		capacite, ok := e.Modele.Composants[b.ComposantOffre]
		if !ok || capacite == 0 {
			return Dimensionnement{}, fmt.Errorf(
				"règle %d (%s) : le modèle %s ne fournit pas « %s »",
				b.RegleID, b.Nom, e.Modele.Code, b.ComposantOffre)
		}

		// 1b. besoin résiduel : ce que l'offre conservée ne couvre pas.
		residuel := math.Max(0, b.Besoin-e.OffreConservee[b.ComposantOffre])
		details = append(details, DetailRegle{
			RegleID:          b.RegleID,
			Nom:              b.Nom,
			Metrique:         b.Metrique,
			Besoin:           b.Besoin,
			BesoinResiduel:   residuel,
			CapaciteUnitaire: capacite,
			NbBrut:           residuel / capacite,
		})
	}

	if len(details) == 0 {
		return Dimensionnement{Details: []DetailRegle{}}, nil
	}

	limitante := details[0]
	for _, d := range details[1:] {
		if d.NbBrut > limitante.NbBrut {
			limitante = d
		}
	}

	repartition := AppliquerContraintes(limitante.NbBrut, e.Contraintes)
	id := limitante.RegleID
	nb := float64(repartition.NbServeurs)

	return Dimensionnement{
		NbServeurs:      repartition.NbServeurs,
		ParZone:         repartition.ParZone,
		NbZones:         repartition.NbZones,
		RegleLimitante:  &id,
		Details:         details,
		CoutAcquisition: nb * e.Modele.PrixFournisseurHT,
		CoutAnnuel:      nb * e.Modele.CoutAnnuelHT,
	}, nil
}
