package depot

import (
	"errors"
	"testing"

	"parallax/internal/capacity"
)

func i64(v int64) *int64 { return &v }

func TestVariableDeclaration(t *testing.T) {
	d := depotDeTest(t)

	v, err := d.CreerVariable(Variable{Code: "taux_remplissage_max", Libelle: "Remplissage max", Defaut: f64(0.7)})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	relu, err := d.LireVariableParCode("taux_remplissage_max")
	if err != nil || relu.ID != v.ID || relu.Defaut == nil || *relu.Defaut != 0.7 {
		t.Fatalf("relecture : %v / %+v", err, relu)
	}
	if _, err := d.CreerVariable(Variable{Code: "", Libelle: "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("attendu ErrValidation, obtenu %v", err)
	}
	if _, err := d.CreerVariable(Variable{Code: "taux_remplissage_max", Libelle: "doublon"}); !errors.Is(err, ErrConflit) {
		t.Fatalf("attendu ErrConflit, obtenu %v", err)
	}
}

func f64(v float64) *float64 { return &v }

func TestVariableValeurHistorique(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	v, _ := d.CreerVariable(Variable{Code: "debit_jour_to", Libelle: "Débit"})

	portee := PorteeVariable{ProjetID: i64(r.Projet), EnvironnementID: i64(r.EnvProd)}
	if _, err := d.DefinirValeur(v.ID, nil, 2026, portee, 50, nil, nil); err != nil {
		t.Fatalf("première valeur : %v", err)
	}
	vv, err := d.DefinirValeur(v.ID, nil, 2026, portee, 65, nil, nil)
	if err != nil {
		t.Fatalf("écrasement : %v", err)
	}

	hist, err := d.HistoriqueValeur(vv.ID)
	if err != nil {
		t.Fatalf("historique : %v", err)
	}
	if len(hist) != 1 || hist[0].Ancienne != 50 || hist[0].Nouvelle != 65 {
		t.Fatalf("historique attendu 50→65, obtenu %+v", hist)
	}

	// réécrire la même valeur ne crée pas d'entrée d'historique
	if _, err := d.DefinirValeur(v.ID, nil, 2026, portee, 65, nil, nil); err != nil {
		t.Fatal(err)
	}
	hist, _ = d.HistoriqueValeur(vv.ID)
	if len(hist) != 1 {
		t.Fatalf("pas de nouvelle entrée attendue, obtenu %d", len(hist))
	}
}

func TestVariableResolution(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	cl := clusterDeTest(t, d.base, r, "ElasticCold1")

	v, _ := d.CreerVariable(Variable{Code: "retention_jours", Libelle: "Rétention", Defaut: f64(1)})

	// valeur au niveau techno, surcharge au niveau cluster
	d.DefinirValeur(v.ID, nil, 2026, PorteeVariable{TechnoID: i64(r.TechnoElastic)}, 30, nil, nil)
	d.DefinirValeur(v.ID, nil, 2026, PorteeVariable{ClusterID: i64(cl)}, 90, nil, nil)

	c := capacity.Cluster{
		ID: cl, ProjetID: i64(r.Projet), EnvironnementID: i64(r.EnvProd),
		TechnoID: i64(r.TechnoElastic), TierID: i64(r.TierCold),
	}
	val, ok, err := d.ResoudreValeur("retention_jours", c, 2026, nil)
	if err != nil || !ok || val != 90 {
		t.Fatalf("résolution la plus spécifique : val=%v ok=%v err=%v", val, ok, err)
	}

	// année sans valeur → défaut de la variable
	val, ok, _ = d.ResoudreValeur("retention_jours", c, 2030, nil)
	if !ok || val != 1 {
		t.Fatalf("défaut attendu 1, obtenu %v (ok=%v)", val, ok)
	}
}
