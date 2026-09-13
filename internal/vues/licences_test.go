package vues

import (
	"testing"

	"parallax/internal/depot"
)

// parcLicences est un jeu de lignes en mémoire, vérifiable à la main :
//
//	techno 1 (ELASTIC), cluster 10 : deux serveurs de 4 nœuds et 768 Go
//	techno 1 (ELASTIC), cluster 11 : un serveur de 2 nœuds et 300 Go
//	techno 2 (KAFKA),   cluster 20 : un serveur de 1 nœud et 128 Go
//
// Avec ram_max = 256 Go : 768 → 3 unités RAM, 300 → 2, 128 → 1.
func parcLicences() []Ligne {
	ligne := func(id, techno, cluster int64, technoCode, projet string, nb, ram float64) Ligne {
		return Ligne{
			ServeurID: id, TechnoID: techno, ClusterID: cluster, TechnoCode: technoCode, ProjetCode: projet,
			NbNoeuds: nb, Quantites: map[string]float64{}, Capacites: map[string]float64{"ram": ram},
		}
	}
	return []Ligne{
		ligne(1, 1, 10, "ELASTIC", "LOGS", 4, 768),
		ligne(2, 1, 10, "ELASTIC", "LOGS", 4, 768),
		ligne(3, 1, 11, "ELASTIC", "STREAM", 2, 300),
		ligne(4, 2, 20, "KAFKA", "LOGS", 1, 128),
	}
}

func contrat(techno int64, mecanisme, niveau string, ramMax, cout float64) depot.LicenceContrat {
	c := depot.LicenceContrat{TechnoID: techno, Annee: 2026, Mecanisme: mecanisme, Niveau: niveau}
	if ramMax > 0 {
		c.RamMaxGo = &ramMax
	}
	if cout > 0 {
		c.CoutUnitaireHT = &cout
	}
	return c
}

// TestCalculerLicencesMecanismeParNiveau parcourt les neuf cases du tableau
// de docs/modele-donnees.md §8.2 sur les lignes ELASTIC seules.
func TestCalculerLicencesMecanismeParNiveau(t *testing.T) {
	elastic := parcLicences()[:3] // ELASTIC seulement
	cas := []struct {
		mecanisme, niveau string
		attendu           float64
		explication       string
	}{
		{depot.MecanismeLicenceNoeuds, depot.NiveauLicenceMachine, 10, "Σ nb_noeuds = 4+4+2"},
		{depot.MecanismeLicenceNoeuds, depot.NiveauLicenceCluster, 10, "idem, niveau sans objet"},
		{depot.MecanismeLicenceNoeuds, depot.NiveauLicenceGlobal, 10, "idem, niveau sans objet"},

		// RAM : ceil(ram/256) par serveur = 3+3+2 ; par cluster = ceil(1536/256)=6 + ceil(300/256)=2 ; global = ceil(1836/256)=8
		{depot.MecanismeLicenceRam, depot.NiveauLicenceMachine, 8, "3+3+2"},
		{depot.MecanismeLicenceRam, depot.NiveauLicenceCluster, 8, "6+2"},
		{depot.MecanismeLicenceRam, depot.NiveauLicenceGlobal, 8, "ceil(1836/256)"},

		// MAX_NOEUDS_RAM : par serveur max(4,3)+max(4,3)+max(2,2) = 10 ;
		// par cluster max(8,6)+max(2,2) = 10 ; global max(10,8) = 10
		{depot.MecanismeLicenceMaxNoeudsRam, depot.NiveauLicenceMachine, 10, "4+4+2"},
		{depot.MecanismeLicenceMaxNoeudsRam, depot.NiveauLicenceCluster, 10, "8+2"},
		{depot.MecanismeLicenceMaxNoeudsRam, depot.NiveauLicenceGlobal, 10, "max(10,8)"},
	}
	for _, c := range cas {
		contrats := map[int64]depot.LicenceContrat{1: contrat(1, c.mecanisme, c.niveau, 256, 100)}
		unites, cout := CalculerLicences(elastic, contrats)
		if unites != c.attendu {
			t.Errorf("%s × %s : attendu %v (%s), obtenu %v", c.mecanisme, c.niveau, c.attendu, c.explication, unites)
		}
		if cout != c.attendu*100 {
			t.Errorf("%s × %s : coût attendu %v, obtenu %v", c.mecanisme, c.niveau, c.attendu*100, cout)
		}
	}
}

// TestCalculerLicencesRamDistingueLesNiveaux utilise des chiffres où les
// trois niveaux divergent, pour prouver qu'ils ne sont pas confondus :
// ram_max = 500 Go sur les mêmes lignes ELASTIC.
func TestCalculerLicencesRamDistingueLesNiveaux(t *testing.T) {
	elastic := parcLicences()[:3]
	cas := map[string]float64{
		depot.NiveauLicenceMachine: 2 + 2 + 1, // ceil(768/500)=2, 2, ceil(300/500)=1
		depot.NiveauLicenceCluster: 4 + 1,     // ceil(1536/500)=4, ceil(300/500)=1
		depot.NiveauLicenceGlobal:  4,         // ceil(1836/500)=4
	}
	for niveau, attendu := range cas {
		unites, _ := CalculerLicences(elastic, map[int64]depot.LicenceContrat{1: contrat(1, depot.MecanismeLicenceRam, niveau, 500, 0)})
		if unites != attendu {
			t.Errorf("RAM × %s : attendu %v, obtenu %v", niveau, attendu, unites)
		}
	}
}

// TestCalculerLicencesTechnoSansContratEtCout : Kafka sans contrat compte 0,
// un contrat sans coût unitaire donne un coût nul, et deux technos sous
// contrat cumulent leurs coûts.
func TestCalculerLicencesTechnoSansContratEtCout(t *testing.T) {
	lignes := parcLicences()

	unites, cout := CalculerLicences(lignes, map[int64]depot.LicenceContrat{
		1: contrat(1, depot.MecanismeLicenceNoeuds, depot.NiveauLicenceMachine, 0, 0),
	})
	if unites != 10 || cout != 0 {
		t.Fatalf("ELASTIC seul sous contrat, sans coût : attendu 10 unités et 0 coût, obtenu %v / %v", unites, cout)
	}

	unites, cout = CalculerLicences(lignes, map[int64]depot.LicenceContrat{
		1: contrat(1, depot.MecanismeLicenceNoeuds, depot.NiveauLicenceMachine, 0, 10),
		2: contrat(2, depot.MecanismeLicenceRam, depot.NiveauLicenceMachine, 64, 5),
	})
	// ELASTIC 10 nœuds × 10 = 100 ; KAFKA ceil(128/64)=2 × 5 = 10
	if unites != 12 || cout != 110 {
		t.Fatalf("deux technos : attendu 12 unités et 110 de coût, obtenu %v / %v", unites, cout)
	}

	if unites, cout := CalculerLicences(lignes, nil); unites != 0 || cout != 0 {
		t.Fatalf("sans contrat : attendu 0, obtenu %v / %v", unites, cout)
	}
}

// TestGlobalNonAdditif : au niveau GLOBAL, la somme des sous-totaux par
// projet dépasse le total du parc entier — c'est la propriété annoncée par
// le backlog, à afficher comme telle et jamais à « corriger ».
func TestGlobalNonAdditif(t *testing.T) {
	elastic := parcLicences()[:3]
	contrats := map[int64]depot.LicenceContrat{1: contrat(1, depot.MecanismeLicenceRam, depot.NiveauLicenceGlobal, 500, 0)}

	total, _ := CalculerLicences(elastic, contrats)      // ceil(1836/500) = 4
	logs, _ := CalculerLicences(elastic[:2], contrats)   // ceil(1536/500) = 4
	stream, _ := CalculerLicences(elastic[2:], contrats) // ceil(300/500) = 1
	if total != 4 || logs != 4 || stream != 1 {
		t.Fatalf("attendu total 4, LOGS 4, STREAM 1 ; obtenu %v, %v, %v", total, logs, stream)
	}
	if logs+stream <= total {
		t.Fatalf("GLOBAL : la somme des sous-totaux (%v) devrait dépasser le total (%v)", logs+stream, total)
	}

	// le même découpage via RegrouperAvecLicences, par projet
	groupes, axes := RegrouperAvecLicences(elastic, []Dimension{DimProjet}, []Colonne{ColLicencesUnites}, contrats)
	if len(axes) != 2 || axes[0] != DimProjet || axes[1] != DimTechno {
		t.Fatalf("l'axe techno doit être ajouté en fin : %v", axes)
	}
	if len(groupes) != 2 || groupes[0].Valeurs[ColLicencesUnites] != 4 || groupes[1].Valeurs[ColLicencesUnites] != 1 {
		t.Fatalf("groupes par projet inattendus : %+v", groupes)
	}
}

// TestRegrouperAvecLicencesAjouteAxeTechno : sans axe techno, le tableau se
// découpe par techno (axe ajouté en fin), et les autres colonnes restent des
// sommes ; avec l'axe déjà présent, rien n'est ajouté ; sans colonne de
// licences, les axes sont inchangés.
func TestRegrouperAvecLicencesAjouteAxeTechno(t *testing.T) {
	lignes := parcLicences()
	contrats := map[int64]depot.LicenceContrat{
		1: contrat(1, depot.MecanismeLicenceRam, depot.NiveauLicenceMachine, 256, 100),
		2: contrat(2, depot.MecanismeLicenceNoeuds, depot.NiveauLicenceMachine, 0, 50),
	}

	// sans axe : un total qui se découpe en deux technos
	groupes, axes := RegrouperAvecLicences(lignes, nil, []Colonne{ColNbServeurs, ColLicencesUnites, ColLicencesCout}, contrats)
	if len(axes) != 1 || axes[0] != DimTechno {
		t.Fatalf("attendu l'axe techno seul, obtenu %v", axes)
	}
	if len(groupes) != 2 {
		t.Fatalf("attendu deux groupes (ELASTIC, KAFKA), obtenu %+v", groupes)
	}
	elastic, kafka := groupes[0], groupes[1] // triés par clé
	if elastic.Cles[0] != "ELASTIC" || kafka.Cles[0] != "KAFKA" {
		t.Fatalf("clés inattendues : %+v", groupes)
	}
	if elastic.Valeurs[ColNbServeurs] != 3 || elastic.Valeurs[ColLicencesUnites] != 8 || elastic.Valeurs[ColLicencesCout] != 800 {
		t.Fatalf("ELASTIC : attendu 3 serveurs, 8 unités, 800 de coût ; obtenu %+v", elastic.Valeurs)
	}
	if kafka.Valeurs[ColNbServeurs] != 1 || kafka.Valeurs[ColLicencesUnites] != 1 || kafka.Valeurs[ColLicencesCout] != 50 {
		t.Fatalf("KAFKA : attendu 1 serveur, 1 unité, 50 de coût ; obtenu %+v", kafka.Valeurs)
	}

	// axe techno déjà présent : inchangé
	_, axes = RegrouperAvecLicences(lignes, []Dimension{DimTechno, DimProjet}, []Colonne{ColLicencesUnites}, contrats)
	if len(axes) != 2 || axes[0] != DimTechno || axes[1] != DimProjet {
		t.Fatalf("axes inchangés attendus, obtenu %v", axes)
	}

	// pas de colonne de licences : axes inchangés, aucun découpage
	groupes, axes = RegrouperAvecLicences(lignes, nil, []Colonne{ColNbServeurs}, contrats)
	if len(axes) != 0 || len(groupes) != 1 || groupes[0].Valeurs[ColNbServeurs] != 4 {
		t.Fatalf("sans colonne de licences : total général attendu, obtenu %v / %+v", axes, groupes)
	}

	// Regrouper seul laisse les colonnes de licences à 0
	groupes = Regrouper(lignes, []Dimension{DimTechno}, []Colonne{ColLicencesUnites})
	for _, g := range groupes {
		if g.Valeurs[ColLicencesUnites] != 0 {
			t.Fatalf("Regrouper sans contrats doit laisser les licences à 0 : %+v", g)
		}
	}
}

func TestPlafondQuotient(t *testing.T) {
	cas := []struct{ ram, ramMax, attendu float64 }{
		{768, 256, 3}, {300, 256, 2}, {0, 256, 0}, {128, 0, 0},
		{0.1 + 0.2, 0.1, 3}, // bruit flottant : 0.30000000000000004 / 0.1 → 3, pas 4
	}
	for _, c := range cas {
		if got := plafondQuotient(c.ram, c.ramMax); got != c.attendu {
			t.Errorf("plafondQuotient(%v, %v) = %v, attendu %v", c.ram, c.ramMax, got, c.attendu)
		}
	}
}
