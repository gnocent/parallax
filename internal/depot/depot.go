// Package depot est la couche d'accès aux données de Parallax : un dépôt par
// entité, au-dessus de la base SQLite ouverte par internal/db.
//
// Conventions communes à tous les dépôts de ce paquet — les respecter pour que
// l'ensemble reste homogène :
//
//   - Un fichier par entité (projet.go, cluster.go…), avec la structure Go
//     correspondant aux colonnes ; les colonnes nullables sont des pointeurs.
//   - Les méthodes portent des noms métier en français : Creer, Lire, Lister,
//     Modifier, et les transitions d'état propres à l'entité (Archiver,
//     Reactiver, Promouvoir…). Rien n'est jamais détruit sans raison : les
//     référentiels se désactivent (actif = 0) plutôt que de se supprimer.
//   - Toute lecture d'une donnée indexée par scénario accepte un *int64 de
//     scénario et résout « surcharge du scénario, à défaut le réel ».
//   - Les vérifications d'invariant qui exigent un lire-puis-écrire atomique
//     passent par enTx. Les helpers internes prennent un conn, satisfait aussi
//     bien par *sql.DB que par *sql.Tx.
//   - Les erreurs remontées sont celles de erreurs.go (ErrIntrouvable,
//     ErrConflit, ErrImmuable, ErrChevauchement, ErrInvariant), enveloppées
//     avec %w. Les erreurs de contrainte SQLite sont traduites par traduire.
//   - Les horodatages d'audit (cree_le, modifie_le) sont posés par le dépôt
//     via Horodatage(). Les dates métier (date_debut, date_effet…) sont
//     fournies par l'appelant et validées par ValiderDate.
package depot

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Depot est le point d'accès à la base. Les dépôts par entité sont des vues
// sur cette même structure ; ils partagent la connexion et le pool.
type Depot struct {
	base   *sql.DB
	auteur *int64 // utilisateur des écritures journalisées, posé par Au ; nil = sans auteur
}

// Nouveau construit un Depot sur une base déjà ouverte et migrée par
// internal/db.Ouvrir.
func Nouveau(base *sql.DB) *Depot {
	return &Depot{base: base}
}

// Base expose la connexion sous-jacente pour les besoins qui débordent des
// dépôts (moteur de capacité alimenté par des requêtes d'agrégat, sauvegarde).
func (d *Depot) Base() *sql.DB { return d.base }

// conn est l'ensemble commun à *sql.DB et *sql.Tx : les helpers internes de
// chaque dépôt s'écrivent contre cette interface pour être réutilisables aussi
// bien hors transaction que dedans.
type conn interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// enTx exécute fn dans une transaction, avec rollback garanti en cas d'erreur
// ou de panique et commit sinon. C'est le point d'entrée de toute opération
// qui vérifie un invariant avant d'écrire.
func (d *Depot) enTx(fn func(tx *sql.Tx) error) error {
	tx, err := d.base.Begin()
	if err != nil {
		return fmt.Errorf("ouverture de la transaction : %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("validation de la transaction : %w", err)
	}
	return nil
}

// Horodatage renvoie l'instant courant en UTC au format RFC3339, pour les
// colonnes d'audit. Redéfinissable dans les tests pour figer le temps.
var Horodatage = func() string { return time.Now().UTC().Format(time.RFC3339) }

// ValiderDate vérifie qu'une date métier est au format ISO-8601 YYYY-MM-DD.
// Une chaîne vide est refusée ; utiliser un *string nil pour « pas de date ».
func ValiderDate(champ, valeur string) error {
	if _, err := time.Parse("2006-01-02", valeur); err != nil {
		return fmt.Errorf("%w : %s « %s » n'est pas une date YYYY-MM-DD",
			ErrValidation, champ, valeur)
	}
	return nil
}

// ValiderDateOpt applique ValiderDate à un pointeur, en acceptant nil.
func ValiderDateOpt(champ string, valeur *string) error {
	if valeur == nil {
		return nil
	}
	return ValiderDate(champ, *valeur)
}

// scanUn réduit le bruit des lectures unitaires : il transforme sql.ErrNoRows
// en ErrIntrouvable enveloppée du contexte fourni.
func scanUn(row *sql.Row, contexte string, cibles ...any) error {
	err := row.Scan(cibles...)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s : %w", contexte, ErrIntrouvable)
	}
	if err != nil {
		return fmt.Errorf("%s : %w", contexte, err)
	}
	return nil
}

// exigerUneLigne renvoie ErrIntrouvable si l'ordre UPDATE/DELETE n'a touché
// aucune ligne — typiquement un identifiant inexistant.
func exigerUneLigne(res sql.Result, contexte string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s : %w", contexte, err)
	}
	if n == 0 {
		return fmt.Errorf("%s : %w", contexte, ErrIntrouvable)
	}
	return nil
}

// boolInt convertit un booléen Go en 0/1 pour les colonnes INTEGER de SQLite,
// où l'on préfère un littéral explicite au binding d'un bool.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// basculerActif pose la colonne actif d'un référentiel à 0 ou 1. La table est
// un littéral contrôlé par l'appelant (jamais une entrée utilisateur).
func basculerActif(c conn, table string, id int64, actif bool) error {
	valeur := 0
	if actif {
		valeur = 1
	}
	res, err := c.Exec(
		fmt.Sprintf(`UPDATE %s SET actif = ? WHERE id = ?`, table), valeur, id)
	if err != nil {
		return traduire(fmt.Sprintf("changement d'activité de %s %d", table, id), err)
	}
	return exigerUneLigne(res, fmt.Sprintf("changement d'activité de %s %d", table, id))
}
