package depot

import (
	"errors"
	"testing"
)

func TestVlanCRUDEtUniciteDuCode(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)

	v, err := d.CreerVlan(Vlan{Code: " V-PROD ", ProjetID: ptrI64(r.Projet), Commentaire: ptrStr("  ")})
	if err != nil {
		t.Fatal(err)
	}
	if v.ID == 0 || v.Code != "V-PROD" || v.Commentaire != nil {
		t.Fatalf("VLAN créé inattendu : %+v", v)
	}
	if _, err := d.CreerVlan(Vlan{Code: "V-PROD"}); !errors.Is(err, ErrConflit) {
		t.Fatalf("code dupliqué : ErrConflit attendue, obtenu %v", err)
	}
	if _, err := d.CreerVlan(Vlan{Code: ""}); !errors.Is(err, ErrValidation) {
		t.Fatalf("code vide : ErrValidation attendue, obtenu %v", err)
	}
	if _, err := d.CreerVlan(Vlan{Code: "X", ProjetID: ptrI64(999)}); !errors.Is(err, ErrReference) {
		t.Fatalf("projet inexistant : ErrReference attendue, obtenu %v", err)
	}
	if _, err := d.CreerVlan(Vlan{Code: "X", DerniereIP: ptrStr("::1")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("curseur IPv6 : ErrValidation attendue, obtenu %v", err)
	}

	lu, err := d.LireVlanParCode("V-PROD")
	if err != nil || lu.ID != v.ID || lu.ProjetID == nil || *lu.ProjetID != r.Projet {
		t.Fatalf("lecture par code : %+v, %v", lu, err)
	}

	lu.Code = "V-PROD2"
	lu.ZoneID = ptrI64(r.DC1)
	lu.DerniereIP = ptrStr("10.0.0.5")
	if err := d.ModifierVlan(lu); err != nil {
		t.Fatal(err)
	}
	relu, _ := d.LireVlan(v.ID)
	if relu.Code != "V-PROD2" || relu.ZoneID == nil || relu.DerniereIP == nil || *relu.DerniereIP != "10.0.0.5" {
		t.Fatalf("modification non appliquée : %+v", relu)
	}
	if err := d.ModifierVlan(Vlan{ID: 999, Code: "Z"}); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification d'un inconnu : ErrIntrouvable attendue, obtenu %v", err)
	}

	if err := d.SupprimerVlan(v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LireVlan(v.ID); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("après suppression : ErrIntrouvable attendue, obtenu %v", err)
	}
}

func TestVlanSupprimerRefuseSiPorteParUnServeur(t *testing.T) {
	d := depotDeTest(t)
	v, err := d.CreerVlan(Vlan{Code: "V1"})
	if err != nil {
		t.Fatal(err)
	}
	exec(t, d.base, `INSERT INTO serveur (physical_name, statut, vlan_id) VALUES ('srv1', 'EN_SERVICE', ?)`, v.ID)
	if err := d.SupprimerVlan(v.ID); !errors.Is(err, ErrReference) {
		t.Fatalf("VLAN porté par un serveur : ErrReference attendue, obtenu %v", err)
	}
}

func TestPlagesValidationChevauchementEtCascade(t *testing.T) {
	d := depotDeTest(t)
	v, err := d.CreerVlan(Vlan{Code: "V1"})
	if err != nil {
		t.Fatal(err)
	}

	p1, err := d.CreerPlage(PlageIP{VlanID: v.ID, IPDebut: " 10.0.0.10 ", IPFin: "10.0.0.20"})
	if err != nil || p1.ID == 0 || p1.IPDebut != "10.0.0.10" {
		t.Fatalf("création de plage : %+v, %v", p1, err)
	}
	cas := []struct {
		debut, fin string
		attendu    error
	}{
		{"10.0.0.20", "10.0.0.10", ErrValidation},     // début > fin
		{"10.0.0.x", "10.0.0.30", ErrValidation},      // borne invalide
		{"2001:db8::1", "2001:db8::9", ErrValidation}, // IPv6
		{"10.0.0.15", "10.0.0.30", ErrChevauchement},  // recouvre p1
		{"10.0.0.1", "10.0.0.10", ErrChevauchement},   // touche la borne de p1
	}
	for _, c := range cas {
		if _, err := d.CreerPlage(PlageIP{VlanID: v.ID, IPDebut: c.debut, IPFin: c.fin}); !errors.Is(err, c.attendu) {
			t.Errorf("plage %s–%s : attendu %v, obtenu %v", c.debut, c.fin, c.attendu, err)
		}
	}
	if _, err := d.CreerPlage(PlageIP{VlanID: 999, IPDebut: "10.0.0.1", IPFin: "10.0.0.2"}); !errors.Is(err, ErrReference) {
		t.Fatalf("VLAN inexistant : ErrReference attendue, obtenu %v", err)
	}

	// une plage disjointe, avant la première : la liste revient triée numériquement
	p0, err := d.CreerPlage(PlageIP{VlanID: v.ID, IPDebut: "10.0.0.2", IPFin: "10.0.0.9"})
	if err != nil {
		t.Fatal(err)
	}
	// un autre VLAN peut recouvrir : le chevauchement n'est refusé qu'au sein d'un VLAN
	v2, _ := d.CreerVlan(Vlan{Code: "V2"})
	if _, err := d.CreerPlage(PlageIP{VlanID: v2.ID, IPDebut: "10.0.0.1", IPFin: "10.0.0.255"}); err != nil {
		t.Fatalf("recouvrement entre VLAN distincts doit passer : %v", err)
	}

	lu, _ := d.LireVlan(v.ID)
	if len(lu.Plages) != 2 || lu.Plages[0].ID != p0.ID || lu.Plages[1].ID != p1.ID {
		t.Fatalf("plages attendues triées [p0 p1], obtenu %+v", lu.Plages)
	}

	// modification : mêmes règles, la plage elle-même exclue du contrôle
	if err := d.ModifierPlage(PlageIP{ID: p1.ID, IPDebut: "10.0.0.10", IPFin: "10.0.0.25"}); err != nil {
		t.Fatalf("étendre sa propre plage doit passer : %v", err)
	}
	if err := d.ModifierPlage(PlageIP{ID: p1.ID, IPDebut: "10.0.0.9", IPFin: "10.0.0.25"}); !errors.Is(err, ErrChevauchement) {
		t.Fatalf("recouvrir p0 : ErrChevauchement attendue, obtenu %v", err)
	}

	if err := d.SupprimerPlage(p0.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.SupprimerPlage(p0.ID); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("seconde suppression : ErrIntrouvable attendue, obtenu %v", err)
	}

	// la suppression du VLAN emporte ses plages (cascade)
	if err := d.SupprimerVlan(v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LirePlage(p1.ID); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("plage orpheline après suppression du VLAN : %v", err)
	}

	vlans, _ := d.ListerVlans()
	if len(vlans) != 1 || vlans[0].Code != "V2" || len(vlans[0].Plages) != 1 {
		t.Fatalf("liste attendue [V2 avec une plage], obtenu %+v", vlans)
	}
}

func TestVlansApplicablesDuPlusSpecifiqueAuMoins(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	cl := clusterDeTest(t, d.base, r, "ElasticHot")
	autre := clusterDeTest(t, d.base, r, "Kafka")

	creer := func(v Vlan) Vlan {
		t.Helper()
		out, err := d.CreerVlan(v)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	creer(Vlan{Code: "GLOBAL"})
	creer(Vlan{Code: "PROD", EnvironnementID: ptrI64(r.EnvProd)})
	creer(Vlan{Code: "QUAL", EnvironnementID: ptrI64(r.EnvQual)})
	creer(Vlan{Code: "LOGS-PROD-DC1", ProjetID: ptrI64(r.Projet), EnvironnementID: ptrI64(r.EnvProd), ZoneID: ptrI64(r.DC1)})
	creer(Vlan{Code: "LOGS-PROD-DC2", ProjetID: ptrI64(r.Projet), EnvironnementID: ptrI64(r.EnvProd), ZoneID: ptrI64(r.DC2)})
	creer(Vlan{Code: "CLUSTER-KAFKA", ClusterID: ptrI64(autre)})
	creer(Vlan{Code: "CLUSTER-HOT", ClusterID: ptrI64(cl)})

	codes := func(vs []Vlan) []string {
		out := make([]string, len(vs))
		for i, v := range vs {
			out[i] = v.Code
		}
		return out
	}

	// serveur LOGS/PROD en DC1 sur ElasticHot
	vs, err := d.VlansApplicables(r.Projet, r.EnvProd, ptrI64(r.DC1), cl)
	if err != nil {
		t.Fatal(err)
	}
	if got := codes(vs); len(got) != 4 || got[0] != "LOGS-PROD-DC1" || got[1] != "CLUSTER-HOT" || got[2] != "PROD" || got[3] != "GLOBAL" {
		t.Fatalf("ordre attendu [LOGS-PROD-DC1 CLUSTER-HOT PROD GLOBAL], obtenu %v", got)
	}

	// serveur sans zone : les VLAN à critère de zone ne s'appliquent pas
	vs, _ = d.VlansApplicables(r.Projet, r.EnvProd, nil, cl)
	if got := codes(vs); len(got) != 3 || got[0] != "CLUSTER-HOT" {
		t.Fatalf("sans zone : attendu [CLUSTER-HOT PROD GLOBAL], obtenu %v", got)
	}

	// autre projet, en QUAL : seuls les critères vides ou QUAL passent
	vs, _ = d.VlansApplicables(r.Projet2, r.EnvQual, ptrI64(r.DC1), autre)
	if got := codes(vs); len(got) != 3 || got[0] != "CLUSTER-KAFKA" || got[1] != "QUAL" || got[2] != "GLOBAL" {
		t.Fatalf("QUAL/Kafka : attendu [CLUSTER-KAFKA QUAL GLOBAL], obtenu %v", got)
	}

	// Applicable (en mémoire) suit la même règle que la requête
	tous, _ := d.ListerVlans()
	for _, v := range tous {
		enSQL := false
		for _, a := range vs {
			if a.ID == v.ID {
				enSQL = true
			}
		}
		if v.Applicable(r.Projet2, r.EnvQual, ptrI64(r.DC1), autre) != enSQL {
			t.Errorf("Applicable(%s) diverge de VlansApplicables", v.Code)
		}
	}
}
