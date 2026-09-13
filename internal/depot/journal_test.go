package depot

import (
	"strings"
	"testing"
	"time"
)

// TestJournalEcritureLectureEtPurge exerce le cœur du journal (v3.4) sans
// passer par un dépôt d'entité : écriture avec et sans auteur, lecture
// filtrée, historique d'une entité, purge au-delà de la rétention.
func TestJournalEcritureLectureEtPurge(t *testing.T) {
	d := depotDeTest(t)
	hash := "x"
	u, err := d.CreerUtilisateur(Utilisateur{Login: "alice", Hash: hash, Role: RoleEditeur})
	if err != nil {
		t.Fatal(err)
	}

	avant := Projet{ID: 7, Code: "LOGS", Libelle: "Avant"}
	apres := Projet{ID: 7, Code: "LOGS", Libelle: "Après"}
	if err := d.Au(u.ID).journaliser(d.base, "projet", 7, ActionModification, avant, apres); err != nil {
		t.Fatal(err)
	}
	if err := d.journaliser(d.base, "projet", 8, ActionCreation, nil, Projet{ID: 8, Code: "X"}); err != nil {
		t.Fatal(err)
	}

	// la création du compte de test est elle-même journalisée (sans auteur,
	// et sans le hash) : on ne regarde que les entrées projet.
	tout, err := d.ListerJournal(FiltreJournal{Entite: "projet"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tout) != 2 || tout[0].EntiteID != 8 || tout[1].EntiteID != 7 {
		t.Fatalf("attendu deux entrées projet, la plus récente d'abord : %+v", tout)
	}
	if compte, _ := d.ListerJournal(FiltreJournal{Entite: "utilisateur"}); len(compte) != 1 || strings.Contains(*compte[0].Apres, "hash") || strings.Contains(*compte[0].Apres, "Hash") {
		t.Fatalf("la création du compte doit être journalisée sans son hash : %+v", compte)
	}
	modif := tout[1]
	if modif.UtilisateurLogin == nil || *modif.UtilisateurLogin != "alice" || modif.Action != ActionModification {
		t.Fatalf("auteur ou action inattendus : %+v", modif)
	}
	if modif.Avant == nil || !strings.Contains(*modif.Avant, `"Libelle":"Avant"`) ||
		modif.Apres == nil || !strings.Contains(*modif.Apres, `"Libelle":"Après"`) {
		t.Fatalf("états avant/après attendus en JSON : %+v", modif)
	}
	creation := tout[0]
	if creation.UtilisateurID != nil || creation.Avant != nil || creation.Apres == nil {
		t.Fatalf("création sans auteur : avant nil, après renseigné : %+v", creation)
	}

	hist, err := d.HistoriqueEntite("projet", 7)
	if err != nil || len(hist) != 1 || hist[0].ID != modif.ID {
		t.Fatalf("historique de l'entité 7 attendu avec une entrée : %+v, %v", hist, err)
	}
	parAuteur, _ := d.ListerJournal(FiltreJournal{UtilisateurID: &u.ID})
	if len(parAuteur) != 1 {
		t.Fatalf("filtre par auteur : attendu 1, obtenu %d", len(parAuteur))
	}
	parAction, _ := d.ListerJournal(FiltreJournal{Entite: "projet", Action: ActionCreation})
	if len(parAction) != 1 || parAction[0].EntiteID != 8 {
		t.Fatalf("filtre par action : %+v", parAction)
	}
	aujourdhui := time.Now().UTC().Format("2006-01-02")
	parDate, _ := d.ListerJournal(FiltreJournal{Depuis: aujourdhui, Jusqua: aujourdhui})
	if len(parDate) != 3 {
		t.Fatalf("filtre par date du jour : attendu 3, obtenu %d", len(parDate))
	}

	// une entrée vieille de 801 jours est purgée, les autres restent.
	ancien := Horodatage
	Horodatage = func() string { return time.Now().UTC().Add(-801 * 24 * time.Hour).Format(time.RFC3339) }
	if err := d.journaliser(d.base, "projet", 9, ActionSuppression, Projet{ID: 9}, nil); err != nil {
		t.Fatal(err)
	}
	Horodatage = ancien
	n, err := d.PurgerJournal(RetentionJournal)
	if err != nil || n != 1 {
		t.Fatalf("purge : attendu 1 entrée supprimée, obtenu %d (%v)", n, err)
	}
	if reste, _ := d.ListerJournal(FiltreJournal{}); len(reste) != 3 {
		t.Fatalf("après purge : attendu 3 entrées, obtenu %d", len(reste))
	}
}

// TestActiviteDepuis vérifie la détection utilisée par la sauvegarde locale
// automatique : une écriture métier après t compte, une écriture sur
// l'entité "parametre" (réglages de la sauvegarde elle-même) ne compte pas —
// sans quoi chaque vérification s'auto-déclarerait changée.
func TestActiviteDepuis(t *testing.T) {
	d := depotDeTest(t)
	référence := time.Now().UTC()

	actif, err := d.ActiviteDepuis(référence)
	if err != nil {
		t.Fatal(err)
	}
	if actif {
		t.Fatal("rien écrit depuis la référence : attendu aucune activité")
	}

	if err := d.DefinirParametre(ParametreSauvegardeHeure, "02:00", nil); err != nil {
		t.Fatal(err)
	}
	actif, err = d.ActiviteDepuis(référence)
	if err != nil {
		t.Fatal(err)
	}
	if actif {
		t.Fatal("un réglage de sauvegarde ne doit pas compter comme une activité")
	}

	// Horodatage() est à la seconde près (comme partout ailleurs dans le
	// journal) : on s'assure de changer de seconde avant l'écriture qui doit
	// être détectée, sans quoi le test serait occasionnellement instable.
	time.Sleep(1100 * time.Millisecond)

	if _, err := d.CreerProjet(Projet{Code: "LOGS", Libelle: "Logs"}); err != nil {
		t.Fatal(err)
	}
	actif, err = d.ActiviteDepuis(référence)
	if err != nil {
		t.Fatal(err)
	}
	if !actif {
		t.Fatal("une écriture métier après la référence doit compter comme une activité")
	}
}
