package depot

import (
	"errors"
	"testing"
)

func ptrI64(v int64) *int64   { return &v }
func ptrStr(v string) *string { return &v }

func TestClusterCycleDeVie(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)

	c, err := d.CreerCluster(Cluster{
		Nom:                "  ElasticCold1  ",
		ProjetID:           r.Projet,
		EnvironnementID:    r.EnvProd,
		TechnoID:           r.TechnoElastic,
		TierID:             ptrI64(r.TierCold),
		UsageFonctionnelID: ptrI64(r.UsageLog),
		Commentaire:        ptrStr("stockage froid"),
	})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if c.ID == 0 {
		t.Fatal("identifiant non attribué")
	}
	if c.Nom != "ElasticCold1" {
		t.Fatalf("nom non normalisé : %q", c.Nom)
	}
	if !c.Actif {
		t.Fatal("un cluster neuf doit être actif")
	}

	relu, err := d.LireCluster(c.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if relu.Nom != "ElasticCold1" || relu.TierID == nil || *relu.TierID != r.TierCold {
		t.Fatalf("relecture divergente : %+v", relu)
	}

	c.Nom = "ElasticCold2"
	c.TierID = nil
	c.Commentaire = nil
	if err := d.ModifierCluster(c); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ = d.LireCluster(c.ID)
	if relu.Nom != "ElasticCold2" || relu.TierID != nil || relu.Commentaire != nil {
		t.Fatalf("modification non persistée : %+v", relu)
	}

	if err := d.ArchiverCluster(c.ID); err != nil {
		t.Fatalf("archivage : %v", err)
	}
	if relu, _ := d.LireCluster(c.ID); relu.Actif {
		t.Fatal("archivage non persisté")
	}
	if err := d.ReactiverCluster(c.ID); err != nil {
		t.Fatalf("réactivation : %v", err)
	}
	if relu, _ := d.LireCluster(c.ID); !relu.Actif {
		t.Fatal("réactivation non persistée")
	}
}

func TestClusterUnicite(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	base := Cluster{Nom: "ElasticHot", ProjetID: r.Projet, EnvironnementID: r.EnvProd, TechnoID: r.TechnoElastic}

	if _, err := d.CreerCluster(base); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerCluster(base); !errors.Is(err, ErrConflit) {
		t.Fatalf("doublon (projet,env,nom) : attendu ErrConflit, obtenu %v", err)
	}

	// même nom, autre environnement : autorisé
	base.EnvironnementID = r.EnvQual
	if _, err := d.CreerCluster(base); err != nil {
		t.Fatalf("même nom dans un autre environnement doit être permis : %v", err)
	}
}

func TestClusterValidation(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	for nom, c := range map[string]Cluster{
		"nom vide":        {Nom: "  ", ProjetID: r.Projet, EnvironnementID: r.EnvProd, TechnoID: r.TechnoElastic},
		"projet absent":   {Nom: "X", EnvironnementID: r.EnvProd, TechnoID: r.TechnoElastic},
		"environnement 0": {Nom: "X", ProjetID: r.Projet, TechnoID: r.TechnoElastic},
		"techno absente":  {Nom: "X", ProjetID: r.Projet, EnvironnementID: r.EnvProd},
	} {
		if _, err := d.CreerCluster(c); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s : attendu ErrValidation, obtenu %v", nom, err)
		}
	}
}

func TestClusterReference(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	_, err := d.CreerCluster(Cluster{
		Nom: "X", ProjetID: 999, EnvironnementID: r.EnvProd, TechnoID: r.TechnoElastic,
	})
	if !errors.Is(err, ErrReference) {
		t.Fatalf("projet inexistant : attendu ErrReference, obtenu %v", err)
	}
}

func TestClusterIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireCluster(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierCluster(Cluster{ID: 404, Nom: "X", ProjetID: 1, EnvironnementID: 1, TechnoID: 1}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ArchiverCluster(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("archivage : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestClusterListerFiltre(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)

	mk := func(nom string, env, techno int64, tier *int64) int64 {
		c, err := d.CreerCluster(Cluster{
			Nom: nom, ProjetID: r.Projet, EnvironnementID: env, TechnoID: techno, TierID: tier,
		})
		if err != nil {
			t.Fatalf("création %s : %v", nom, err)
		}
		return c.ID
	}
	hot := mk("ElasticHot", r.EnvProd, r.TechnoElastic, ptrI64(r.TierHot))
	mk("ElasticCold", r.EnvProd, r.TechnoElastic, ptrI64(r.TierCold))
	kafka := mk("Kafka", r.EnvProd, r.TechnoKafka, nil)
	mk("ElasticHotQual", r.EnvQual, r.TechnoElastic, ptrI64(r.TierHot))

	// tri par nom, tous actifs
	tous, err := d.ListerClusters(FiltreCluster{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tous) != 4 || tous[0].Nom != "ElasticCold" || tous[3].Nom != "Kafka" {
		t.Fatalf("tri par nom incorrect : %+v", noms(tous))
	}

	// filtre techno
	elastic, _ := d.ListerClusters(FiltreCluster{TechnoID: ptrI64(r.TechnoElastic)})
	if len(elastic) != 3 {
		t.Fatalf("filtre techno : %+v", noms(elastic))
	}

	// filtre combiné techno + environnement + tier
	sel, _ := d.ListerClusters(FiltreCluster{
		TechnoID: ptrI64(r.TechnoElastic), EnvironnementID: ptrI64(r.EnvProd), TierID: ptrI64(r.TierHot),
	})
	if len(sel) != 1 || sel[0].ID != hot {
		t.Fatalf("filtre combiné : %+v", noms(sel))
	}

	// inactifs exclus par défaut, inclus sur demande
	if err := d.ArchiverCluster(kafka); err != nil {
		t.Fatal(err)
	}
	actifs, _ := d.ListerClusters(FiltreCluster{})
	if len(actifs) != 3 {
		t.Fatalf("archivé encore listé : %+v", noms(actifs))
	}
	avecInactifs, _ := d.ListerClusters(FiltreCluster{InclureInactifs: true})
	if len(avecInactifs) != 4 {
		t.Fatalf("InclureInactifs ignoré : %+v", noms(avecInactifs))
	}
}

func noms(cs []Cluster) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Nom
	}
	return out
}
