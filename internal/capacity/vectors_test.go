package capacity

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Ces tests sont pilotés par spec/vectors.json, produit par
// spec/capacity_reference.py. La référence Python fait foi : toute
// divergence est un bug Go, sauf décision explicite de faire évoluer la
// spécification — auquel cas on modifie le Python d'abord et on régénère.

const tolerance = 1e-6

type vecteurs struct {
	Expressions []struct {
		Expression string             `json:"expression"`
		Variables  map[string]float64 `json:"variables"`
		Attendu    *float64           `json:"attendu"`
		Erreur     string             `json:"erreur"`
	} `json:"expressions"`

	Resolution []struct {
		Libelle    string           `json:"libelle"`
		Code       string           `json:"code"`
		Cluster    Cluster          `json:"cluster"`
		Annee      int              `json:"annee"`
		ScenarioID *int64           `json:"scenario_id"`
		Valeurs    []ValeurVariable `json:"valeurs"`
		Defaut     *float64         `json:"defaut"`
		Attendu    *float64         `json:"attendu"`
	} `json:"resolution"`

	Conflits []struct {
		Libelle  string    `json:"libelle"`
		Regles   []Regle   `json:"regles"`
		Clusters []Cluster `json:"clusters"`
		Attendu  []Conflit `json:"attendu"`
	} `json:"conflits"`

	Contraintes []struct {
		Libelle     string      `json:"libelle"`
		NbBrut      float64     `json:"nb_brut"`
		Contraintes Contraintes `json:"contraintes"`
		Attendu     Repartition `json:"attendu"`
	} `json:"contraintes"`

	Dimensionnement []struct {
		Libelle        string             `json:"libelle"`
		Cluster        Cluster            `json:"cluster"`
		Annee          int                `json:"annee"`
		ScenarioID     *int64             `json:"scenario_id"`
		Modele         Modele             `json:"modele"`
		Contraintes    Contraintes        `json:"contraintes"`
		OffreConservee map[string]float64 `json:"offre_conservee"`
		Regles         []Regle            `json:"regles"`
		Valeurs        []ValeurVariable   `json:"valeurs"`
		Defauts        map[string]float64 `json:"defauts"`
		Attendu        Dimensionnement    `json:"attendu"`
	} `json:"dimensionnement"`
}

func charger(t *testing.T) vecteurs {
	t.Helper()
	chemin := filepath.Join("..", "..", "spec", "vectors.json")
	brut, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatalf("lecture de %s : %v\n"+
			"exécuter d'abord : python3 spec/capacity_reference.py", chemin, err)
	}
	var v vecteurs
	if err := json.Unmarshal(brut, &v); err != nil {
		t.Fatalf("vectors.json illisible : %v", err)
	}
	return v
}

func proche(a, b float64) bool { return math.Abs(a-b) <= tolerance }

func entiersEgaux(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TestExpressions(t *testing.T) {
	for _, cas := range charger(t).Expressions {
		cas := cas
		t.Run(cas.Expression, func(t *testing.T) {
			obtenu, err := Evaluer(cas.Expression, cas.Variables)
			if cas.Attendu == nil {
				if err == nil {
					t.Fatalf("erreur attendue (%s), obtenu %v", cas.Erreur, obtenu)
				}
				return
			}
			if err != nil {
				t.Fatalf("erreur inattendue : %v", err)
			}
			if !proche(obtenu, *cas.Attendu) {
				t.Fatalf("attendu %g, obtenu %g", *cas.Attendu, obtenu)
			}
		})
	}
}

func TestResolutionVariables(t *testing.T) {
	for _, cas := range charger(t).Resolution {
		cas := cas
		t.Run(cas.Libelle, func(t *testing.T) {
			obtenu, trouve := ResoudreVariable(cas.Valeurs, cas.Code, cas.Cluster,
				cas.Annee, cas.ScenarioID)
			if !trouve {
				if cas.Defaut != nil {
					obtenu, trouve = *cas.Defaut, true
				}
			}
			if cas.Attendu == nil {
				if trouve {
					t.Fatalf("aucune valeur attendue, obtenu %g", obtenu)
				}
				return
			}
			if !trouve {
				t.Fatalf("attendu %g, aucune valeur résolue", *cas.Attendu)
			}
			if !proche(obtenu, *cas.Attendu) {
				t.Fatalf("attendu %g, obtenu %g", *cas.Attendu, obtenu)
			}
		})
	}
}

func TestDetectionConflits(t *testing.T) {
	for _, cas := range charger(t).Conflits {
		cas := cas
		t.Run(cas.Libelle, func(t *testing.T) {
			obtenus := DetecterConflits(cas.Regles, cas.Clusters)
			if len(obtenus) != len(cas.Attendu) {
				t.Fatalf("attendu %d conflits, obtenu %d : %+v",
					len(cas.Attendu), len(obtenus), obtenus)
			}
			for i, attendu := range cas.Attendu {
				o := obtenus[i]
				if o.RegleA != attendu.RegleA || o.RegleB != attendu.RegleB {
					t.Fatalf("conflit %d : attendu (%d,%d), obtenu (%d,%d)",
						i, attendu.RegleA, attendu.RegleB, o.RegleA, o.RegleB)
				}
				if len(o.Clusters) != len(attendu.Clusters) {
					t.Fatalf("conflit %d : clusters attendus %v, obtenus %v",
						i, attendu.Clusters, o.Clusters)
				}
				for j := range attendu.Clusters {
					if o.Clusters[j] != attendu.Clusters[j] {
						t.Fatalf("conflit %d : clusters attendus %v, obtenus %v",
							i, attendu.Clusters, o.Clusters)
					}
				}
			}
		})
	}
}

func TestContraintes(t *testing.T) {
	for _, cas := range charger(t).Contraintes {
		cas := cas
		t.Run(cas.Libelle, func(t *testing.T) {
			obtenu := AppliquerContraintes(cas.NbBrut, cas.Contraintes)
			if obtenu.NbServeurs != cas.Attendu.NbServeurs {
				t.Fatalf("nb_serveurs : attendu %d, obtenu %d",
					cas.Attendu.NbServeurs, obtenu.NbServeurs)
			}
			if !entiersEgaux(obtenu.ParZone, cas.Attendu.ParZone) {
				t.Fatalf("par_zone : attendu %v, obtenu %v",
					cas.Attendu.ParZone, obtenu.ParZone)
			}
			if !entiersEgaux(obtenu.NbZones, cas.Attendu.NbZones) {
				t.Fatalf("nb_zones : attendu %v, obtenu %v",
					cas.Attendu.NbZones, obtenu.NbZones)
			}
		})
	}
}

func TestDimensionnement(t *testing.T) {
	for _, cas := range charger(t).Dimensionnement {
		cas := cas
		t.Run(cas.Libelle, func(t *testing.T) {
			obtenu, err := Dimensionner(EntreeDimensionnement{
				Cluster:        cas.Cluster,
				Regles:         cas.Regles,
				Valeurs:        cas.Valeurs,
				Defauts:        cas.Defauts,
				Annee:          cas.Annee,
				ScenarioID:     cas.ScenarioID,
				Modele:         cas.Modele,
				Contraintes:    cas.Contraintes,
				OffreConservee: cas.OffreConservee,
			})
			if err != nil {
				t.Fatalf("erreur inattendue : %v", err)
			}

			a := cas.Attendu
			if obtenu.NbServeurs != a.NbServeurs {
				t.Fatalf("nb_serveurs : attendu %d, obtenu %d", a.NbServeurs, obtenu.NbServeurs)
			}
			if !entiersEgaux(obtenu.ParZone, a.ParZone) {
				t.Fatalf("par_zone : attendu %v, obtenu %v", a.ParZone, obtenu.ParZone)
			}
			switch {
			case a.RegleLimitante == nil && obtenu.RegleLimitante != nil:
				t.Fatalf("aucune règle limitante attendue, obtenu %d", *obtenu.RegleLimitante)
			case a.RegleLimitante != nil && obtenu.RegleLimitante == nil:
				t.Fatalf("règle limitante %d attendue, aucune obtenue", *a.RegleLimitante)
			case a.RegleLimitante != nil && *a.RegleLimitante != *obtenu.RegleLimitante:
				t.Fatalf("règle limitante : attendu %d, obtenu %d",
					*a.RegleLimitante, *obtenu.RegleLimitante)
			}
			if !proche(obtenu.CoutAcquisition, a.CoutAcquisition) {
				t.Fatalf("coût acquisition : attendu %g, obtenu %g",
					a.CoutAcquisition, obtenu.CoutAcquisition)
			}
			if !proche(obtenu.CoutAnnuel, a.CoutAnnuel) {
				t.Fatalf("coût annuel : attendu %g, obtenu %g", a.CoutAnnuel, obtenu.CoutAnnuel)
			}

			if len(obtenu.Details) != len(a.Details) {
				t.Fatalf("détails : attendu %d règles, obtenu %d", len(a.Details), len(obtenu.Details))
			}
			for i, attendu := range a.Details {
				o := obtenu.Details[i]
				if o.RegleID != attendu.RegleID {
					t.Fatalf("détail %d : règle attendue %d, obtenue %d", i, attendu.RegleID, o.RegleID)
				}
				if !proche(o.Besoin, attendu.Besoin) {
					t.Fatalf("détail %d (%s) : besoin attendu %g, obtenu %g",
						i, attendu.Nom, attendu.Besoin, o.Besoin)
				}
				if !proche(o.BesoinResiduel, attendu.BesoinResiduel) {
					t.Fatalf("détail %d (%s) : besoin résiduel attendu %g, obtenu %g",
						i, attendu.Nom, attendu.BesoinResiduel, o.BesoinResiduel)
				}
				if !proche(o.NbBrut, attendu.NbBrut) {
					t.Fatalf("détail %d (%s) : nb_brut attendu %g, obtenu %g",
						i, attendu.Nom, attendu.NbBrut, o.NbBrut)
				}
			}
		})
	}
}

// TestCalculerBesoins vérifie que le besoin brut par règle, extrait de
// Dimensionner pour l'écran besoin / offre / écart (v1.5), reste identique à
// ce que Dimensionner calculait déjà en interne — pas de vecteur Python ici :
// c'est une extraction de code, pas une nouvelle sémantique numérique.
func TestCalculerBesoins(t *testing.T) {
	cluster := Cluster{ID: 1, TechnoID: i64p(1), TierID: i64p(1)}
	regles := []Regle{
		{ID: 1, Nom: "Disque", Metrique: "DISQUE_UTILE_TO", Expression: "debit * retention",
			NiveauEvaluation: NiveauPerimetre, ComposantOffre: "ssd", Actif: true,
			TechnoID: i64p(1), TierID: i64p(1)},
		{ID: 2, Nom: "RAM par machine", Metrique: "RAM_GO", Expression: "ceil(ram_machine / 64)",
			NiveauEvaluation: NiveauParServeur, ComposantOffre: "ram", Actif: true,
			TechnoID: i64p(1), TierID: i64p(1)},
		{ID: 3, Nom: "Inactive", Metrique: "AUTRE", Expression: "1",
			NiveauEvaluation: NiveauPerimetre, ComposantOffre: "x", Actif: false,
			TechnoID: i64p(1), TierID: i64p(1)},
	}
	valeurs := []ValeurVariable{
		{Code: "debit", Annee: 2027, TechnoID: i64p(1)},
		{Code: "retention", Annee: 2027, TierID: i64p(1)},
	}
	entree := EntreeDimensionnement{
		Cluster: cluster, Regles: regles, Annee: 2027,
		Defauts:  map[string]float64{"debit": 10, "retention": 7, "ram_machine": 0},
		Valeurs:  valeurs,
		Serveurs: []map[string]float64{{"ram_machine": 130}, {"ram_machine": 64}},
	}
	// on force la résolution via les défauts en laissant Valeurs vide pour
	// debit/retention (portée non testée ici, déjà couverte par ailleurs)
	entree.Valeurs = nil

	besoins, err := CalculerBesoins(entree)
	if err != nil {
		t.Fatalf("erreur inattendue : %v", err)
	}
	if len(besoins) != 2 {
		t.Fatalf("attendu 2 besoins (la règle inactive exclue), obtenu %d : %+v", len(besoins), besoins)
	}

	if besoins[0].RegleID != 1 || !proche(besoins[0].Besoin, 70) { // 10 * 7
		t.Fatalf("règle disque : attendu besoin 70, obtenu %+v", besoins[0])
	}
	if besoins[0].ComposantOffre != "ssd" {
		t.Fatalf("composant offre attendu ssd, obtenu %s", besoins[0].ComposantOffre)
	}

	// PAR_SERVEUR : ceil(130/64)=3 + ceil(64/64)=1 = 4
	if besoins[1].RegleID != 2 || !proche(besoins[1].Besoin, 4) {
		t.Fatalf("règle RAM par machine : attendu besoin 4, obtenu %+v", besoins[1])
	}

	// Dimensionner doit produire les mêmes besoins dans ses détails, avec en
	// plus la capacité du modèle candidat.
	dim, err := Dimensionner(EntreeDimensionnement{
		Cluster: cluster, Regles: regles, Annee: 2027,
		Defauts:  map[string]float64{"debit": 10, "retention": 7, "ram_machine": 0},
		Serveurs: entree.Serveurs,
		Modele:   Modele{Code: "STD", Composants: map[string]float64{"ssd": 96, "ram": 64}},
	})
	if err != nil {
		t.Fatalf("Dimensionner : %v", err)
	}
	if len(dim.Details) != 2 || dim.Details[0].Besoin != besoins[0].Besoin || dim.Details[1].Besoin != besoins[1].Besoin {
		t.Fatalf("Dimensionner et CalculerBesoins divergent : %+v vs %+v", dim.Details, besoins)
	}
}

func i64p(v int64) *int64 { return &v }

// TestVariablesExpression vérifie l'extraction des identifiants, utilisée
// pour valider une règle à la saisie.
func TestVariablesExpression(t *testing.T) {
	e, err := Compiler("ceil(debit * retention / max(a, 2)) + abs(b)")
	if err != nil {
		t.Fatal(err)
	}
	attendu := []string{"a", "b", "debit", "retention"}
	obtenu := e.Variables()
	if len(obtenu) != len(attendu) {
		t.Fatalf("attendu %v, obtenu %v", attendu, obtenu)
	}
	for i := range attendu {
		if obtenu[i] != attendu[i] {
			t.Fatalf("attendu %v, obtenu %v", attendu, obtenu)
		}
	}
}

// TestPrecedence fige quelques règles de priorité qui ne sont pas couvertes
// par les vecteurs et dont une régression passerait inaperçue.
func TestPrecedence(t *testing.T) {
	cas := []struct {
		expression string
		attendu    float64
	}{
		{"2 + 3 * 4", 14},
		{"2 * 3 + 4", 10},
		{"2 ^ 3 ^ 2", 512}, // associatif à droite
		{"-2 ^ 2", -4},     // l'unaire s'applique après la puissance
		{"10 - 2 - 3", 5},  // associatif à gauche
		{"10 / 2 / 5", 1},
		{"7 % 3", 1},
	}
	for _, c := range cas {
		obtenu, err := Evaluer(c.expression, nil)
		if err != nil {
			t.Fatalf("%s : %v", c.expression, err)
		}
		if !proche(obtenu, c.attendu) {
			t.Fatalf("%s : attendu %g, obtenu %g", c.expression, c.attendu, obtenu)
		}
	}
}
