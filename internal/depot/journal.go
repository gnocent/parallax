package depot

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Journal des modifications (backlog v3.4, décision du 2026-09-12) : une
// ligne par entité modifiée, avec son état avant et après en JSON, écrite
// dans la transaction de la modification. Il sert à comprendre une erreur,
// rare, jamais à l'annuler : restaurer un instantané contournerait les
// invariants (affectation active unique, révisions immuables, règles sans
// recouvrement). Purge au-delà de RetentionJournal.
//
// L'auteur vient de Depot.Au(utilisateurID) : la couche web dérive, par
// requête, un dépôt portant l'identité connectée ; un dépôt sans auteur
// (démarrage, tests, import en ligne de commande) journalise avec
// utilisateur_id NULL plutôt que de refuser d'écrire.

// Actions journalisées, alignées sur le CHECK de la table.
const (
	ActionCreation     = "CREATION"
	ActionModification = "MODIFICATION"
	ActionCorrection   = "CORRECTION" // écrasement explicite d'une révision référencée
	ActionSuppression  = "SUPPRESSION"
)

// RetentionJournal : au-delà, une entrée est purgeable (800 jours, entretien D).
const RetentionJournal = 800 * 24 * time.Hour

// EntreeJournal est une ligne du journal, avec le login de l'auteur résolu
// pour l'affichage (nil si l'action n'avait pas d'auteur ou si le compte a
// disparu).
type EntreeJournal struct {
	ID               int64
	Entite           string
	EntiteID         int64
	Action           string
	UtilisateurID    *int64
	UtilisateurLogin *string
	Horodatage       string
	Avant            *string // JSON de l'entité avant, nil pour une création
	Apres            *string // JSON de l'entité après, nil pour une suppression
}

// Au renvoie un dépôt identique portant l'auteur des écritures à venir. La
// copie est superficielle et bon marché : même connexion, même pool.
func (d *Depot) Au(utilisateurID int64) *Depot {
	c := *d
	c.auteur = &utilisateurID
	return &c
}

// Auteur renvoie l'utilisateur porté par ce dépôt, nil s'il n'en a pas.
func (d *Depot) Auteur() *int64 { return d.auteur }

// journaliser écrit une entrée dans la transaction courante. avant et apres
// sont sérialisés en JSON ; nil donne NULL. Toute entité journalisée doit
// être une structure du dépôt (ses champs exportés font foi), jamais une
// forme dérivée pour l'écran.
func (d *Depot) journaliser(c conn, entite string, entiteID int64, action string, avant, apres any) error {
	jAvant, err := jsonOuNil(avant)
	if err != nil {
		return fmt.Errorf("journal %s %d : sérialisation de l'état avant : %w", entite, entiteID, err)
	}
	jApres, err := jsonOuNil(apres)
	if err != nil {
		return fmt.Errorf("journal %s %d : sérialisation de l'état après : %w", entite, entiteID, err)
	}
	if _, err := c.Exec(
		`INSERT INTO journal (entite, entite_id, action, utilisateur_id, horodatage, avant, apres)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entite, entiteID, action, d.auteur, Horodatage(), jAvant, jApres); err != nil {
		return traduire(fmt.Sprintf("journal %s %d", entite, entiteID), err)
	}
	return nil
}

// Les trois helpers ci-dessous portent le patron commun des dépôts
// journalisés (fonctions génériques : Go n'admet pas de méthode générique) :
// ouvrir la transaction, relire l'état avant si besoin, exécuter, relire
// l'état après, journaliser — le tout dans la même transaction, donc
// jamais d'écriture sans sa ligne de journal ni l'inverse.

// creerJournalise exécute inserer dans une transaction et journalise la
// création de l'entité renvoyée (son identifiant est celui attribué).
func creerJournalise[T any](d *Depot, entite string, inserer func(tx *sql.Tx) (T, int64, error)) (T, error) {
	var cree T
	err := d.enTx(func(tx *sql.Tx) error {
		v, id, err := inserer(tx)
		if err != nil {
			return err
		}
		cree = v
		return d.journaliser(tx, entite, id, ActionCreation, nil, v)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return cree, nil
}

// modifierJournalise relit l'entité avant (lire), exécute la modification,
// relit après, et journalise sous action (MODIFICATION ou CORRECTION).
func modifierJournalise[T any](d *Depot, entite string, id int64, action string,
	lire func(c conn, id int64) (T, error), executer func(tx *sql.Tx) error) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := lire(tx, id)
		if err != nil {
			return err
		}
		if err := executer(tx); err != nil {
			return err
		}
		apres, err := lire(tx, id)
		if err != nil {
			return err
		}
		return d.journaliser(tx, entite, id, action, avant, apres)
	})
}

// supprimerJournalise relit l'entité, exécute la suppression et journalise
// l'état avant.
func supprimerJournalise[T any](d *Depot, entite string, id int64,
	lire func(c conn, id int64) (T, error), executer func(tx *sql.Tx) error) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := lire(tx, id)
		if err != nil {
			return err
		}
		if err := executer(tx); err != nil {
			return err
		}
		return d.journaliser(tx, entite, id, ActionSuppression, avant, nil)
	})
}

// Instantané générique d'une ligne, pour les dépôts qui n'ont pas de
// lecteur typé sur une connexion (règles, valeurs de variables, affectations,
// plages…) : la ligne entière sous forme de carte colonne → valeur, telle
// qu'elle est en base. Le journal montre alors les colonnes SQL, ce qui est
// précisément ce qu'on veut relire quand on cherche une erreur. La table est
// toujours un littéral du code, jamais une entrée utilisateur.
func instantane(c conn, table string, id int64) (map[string]any, error) {
	lignes, err := c.Query(fmt.Sprintf(`SELECT * FROM %s WHERE id = ?`, table), id)
	if err != nil {
		return nil, fmt.Errorf("instantané %s %d : %w", table, id, err)
	}
	defer lignes.Close()
	if !lignes.Next() {
		return nil, lignes.Err()
	}
	colonnes, err := lignes.Columns()
	if err != nil {
		return nil, err
	}
	valeurs := make([]any, len(colonnes))
	cibles := make([]any, len(colonnes))
	for i := range valeurs {
		cibles[i] = &valeurs[i]
	}
	if err := lignes.Scan(cibles...); err != nil {
		return nil, fmt.Errorf("instantané %s %d : %w", table, id, err)
	}
	out := make(map[string]any, len(colonnes))
	for i, col := range colonnes {
		if col == "hash" {
			continue // un secret n'a rien à faire dans le journal
		}
		if b, ok := valeurs[i].([]byte); ok {
			out[col] = string(b)
		} else {
			out[col] = valeurs[i]
		}
	}
	return out, nil
}

// JournaliserCreation est le point d'entrée des imports (backlog v3.4 : une
// ligne par entité créée) : ils écrivent par SQL direct dans une transaction
// ouverte sur Base(), hors des méthodes du dépôt, et journalisent chaque
// ligne créée par cet appel, dans la même transaction.
func (d *Depot) JournaliserCreation(tx *sql.Tx, entite, table string, id int64) error {
	return d.journalCreationTable(tx, entite, table, id)
}

// journalCreationTable journalise la création de la ligne id de table, dont
// l'état après est lu en base (dans la transaction).
func (d *Depot) journalCreationTable(c conn, entite, table string, id int64) error {
	apres, err := instantane(c, table, id)
	if err != nil {
		return err
	}
	return d.journaliser(c, entite, id, ActionCreation, nil, apres)
}

// journalModificationTable journalise une modification : avant a été pris
// par instantane avant l'écriture, l'état après est relu ici.
func (d *Depot) journalModificationTable(c conn, entite, table string, id int64, action string, avant map[string]any) error {
	apres, err := instantane(c, table, id)
	if err != nil {
		return err
	}
	return d.journaliser(c, entite, id, action, avant, apres)
}

// journalSuppressionTable journalise une suppression à partir de l'état
// avant, pris par instantane avant le DELETE.
func (d *Depot) journalSuppressionTable(c conn, entite string, id int64, avant map[string]any) error {
	return d.journaliser(c, entite, id, ActionSuppression, avant, nil)
}

// ecrireJournalise exécute une écriture ciblant une seule ligne (par id) et
// la journalise — le geste élémentaire des actions composites (promotion
// d'un scénario), qui touchent des dizaines de lignes de tables différentes
// dans une même transaction. action SUPPRESSION ne relit rien après.
func (d *Depot) ecrireJournalise(tx *sql.Tx, entite, table string, id int64, action string, ctx string, requete string, args ...any) error {
	avant, err := instantane(tx, table, id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(requete, args...); err != nil {
		return traduire(ctx, err)
	}
	if action == ActionSuppression {
		return d.journalSuppressionTable(tx, entite, id, avant)
	}
	return d.journalModificationTable(tx, entite, table, id, action, avant)
}

func jsonOuNil(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

// FiltreJournal restreint ListerJournal ; un champ vide ne filtre pas.
// Depuis et Jusqua sont des dates YYYY-MM-DD (bornes incluses) ; Limite 0
// vaut 500.
type FiltreJournal struct {
	Entite        string
	EntiteID      *int64
	UtilisateurID *int64
	Action        string
	Depuis        string
	Jusqua        string
	Limite        int
}

const colonnesJournal = `j.id, j.entite, j.entite_id, j.action, j.utilisateur_id, u.login,
	j.horodatage, j.avant, j.apres`

// ListerJournal renvoie les entrées les plus récentes d'abord.
func (d *Depot) ListerJournal(f FiltreJournal) ([]EntreeJournal, error) {
	var conditions []string
	var args []any
	if f.Entite != "" {
		conditions = append(conditions, "j.entite = ?")
		args = append(args, f.Entite)
	}
	if f.EntiteID != nil {
		conditions = append(conditions, "j.entite_id = ?")
		args = append(args, *f.EntiteID)
	}
	if f.UtilisateurID != nil {
		conditions = append(conditions, "j.utilisateur_id = ?")
		args = append(args, *f.UtilisateurID)
	}
	if f.Action != "" {
		conditions = append(conditions, "j.action = ?")
		args = append(args, f.Action)
	}
	if f.Depuis != "" {
		if err := ValiderDate("depuis", f.Depuis); err != nil {
			return nil, err
		}
		conditions = append(conditions, "j.horodatage >= ?")
		args = append(args, f.Depuis)
	}
	if f.Jusqua != "" {
		if err := ValiderDate("jusqu'à", f.Jusqua); err != nil {
			return nil, err
		}
		conditions = append(conditions, "j.horodatage < ?")
		args = append(args, f.Jusqua+"T24") // strictement avant le lendemain, en texte RFC3339
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}
	limite := f.Limite
	if limite <= 0 {
		limite = 500
	}
	args = append(args, limite)

	lignes, err := d.base.Query(fmt.Sprintf(
		`SELECT %s FROM journal j LEFT JOIN utilisateur u ON u.id = j.utilisateur_id
		 %s ORDER BY j.id DESC LIMIT ?`, colonnesJournal, where), args...)
	if err != nil {
		return nil, traduire("liste du journal", err)
	}
	defer lignes.Close()
	return scannerJournal(lignes)
}

// HistoriqueEntite renvoie les entrées d'une entité, les plus récentes
// d'abord — la section « historique » d'une fiche.
func (d *Depot) HistoriqueEntite(entite string, entiteID int64) ([]EntreeJournal, error) {
	return d.ListerJournal(FiltreJournal{Entite: entite, EntiteID: &entiteID, Limite: 200})
}

func scannerJournal(lignes *sql.Rows) ([]EntreeJournal, error) {
	var out []EntreeJournal
	for lignes.Next() {
		var e EntreeJournal
		if err := lignes.Scan(&e.ID, &e.Entite, &e.EntiteID, &e.Action, &e.UtilisateurID,
			&e.UtilisateurLogin, &e.Horodatage, &e.Avant, &e.Apres); err != nil {
			return nil, fmt.Errorf("lecture du journal : %w", err)
		}
		out = append(out, e)
	}
	return out, lignes.Err()
}

// PurgerJournal supprime les entrées plus anciennes que retention et renvoie
// leur nombre. Appelée au démarrage puis chaque jour (cmd/parallax), et à la
// demande depuis la page du journal.
// ActiviteDepuis indique si au moins une écriture métier a été journalisée
// après t — sert à la sauvegarde locale automatique (cmd/parallax) pour ne
// produire un nouveau fichier que si quelque chose a changé. Les écritures
// sur l'entité "parametre" sont exclues : ce sont les réglages de la
// sauvegarde elle-même (dossier, heure, rétention, état du planificateur),
// jamais une donnée du métier — les compter créerait une boucle où chaque
// vérification s'auto-déclare "changement depuis la dernière fois".
func (d *Depot) ActiviteDepuis(t time.Time) (bool, error) {
	var existe bool
	err := d.base.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM journal WHERE horodatage > ? AND entite != 'parametre')`,
		t.UTC().Format(time.RFC3339)).Scan(&existe)
	if err != nil {
		return false, traduire("activité depuis", err)
	}
	return existe, nil
}

func (d *Depot) PurgerJournal(retention time.Duration) (int64, error) {
	seuil := time.Now().UTC().Add(-retention).Format(time.RFC3339)
	res, err := d.base.Exec(`DELETE FROM journal WHERE horodatage < ?`, seuil)
	if err != nil {
		return 0, traduire("purge du journal", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// EntitesJournal liste les entités connues du journal, pour le filtre de
// la page — alimentée par les dépôts qui journalisent.
var EntitesJournal = []string{
	"projet", "environnement", "techno", "tier", "usage", "zone",
	"modele", "revision", "composant", "modele_noeud",
	"cluster", "serveur", "serveur_revision", "affectation",
	"scenario", "variable", "variable_valeur", "metrique", "regle", "contrainte",
	"utilisateur", "vue", "vlan", "plage_ip", "licence_contrat", "parametre",
}
