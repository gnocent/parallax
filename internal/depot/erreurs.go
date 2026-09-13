package depot

import (
	"errors"
	"fmt"
	"strings"

	sqlite "modernc.org/sqlite"
)

// Erreurs sentinelles du paquet. Les dépôts les enveloppent avec %w et un
// message de contexte ; l'appelant les teste avec errors.Is.
var (
	// ErrIntrouvable : l'entité demandée n'existe pas.
	ErrIntrouvable = errors.New("entité introuvable")

	// ErrConflit : violation d'unicité (code déjà pris, doublon de portée…).
	ErrConflit = errors.New("conflit d'unicité")

	// ErrReference : clé étrangère inexistante, ou entité encore référencée.
	ErrReference = errors.New("référence invalide")

	// ErrValidation : donnée d'entrée mal formée (date, énumération, plage).
	ErrValidation = errors.New("donnée invalide")

	// ErrImmuable : tentative de modifier une révision déjà référencée par un
	// serveur. L'appelant doit soit créer une nouvelle révision, soit demander
	// explicitement une correction d'erreur.
	ErrImmuable = errors.New("révision immuable")

	// ErrChevauchement : deux périodes datées qui se recouvrent alors que
	// l'invariant l'interdit (affectations, rattachements de révision).
	ErrChevauchement = errors.New("chevauchement de périodes")

	// ErrInvariant : un invariant du §12 de docs/modele-donnees.md non couvert
	// par les catégories ci-dessus (chevauchement de règles sur une métrique,
	// serveur HYPOTHESE sans scénario…).
	ErrInvariant = errors.New("invariant violé")
)

// Codes de résultat étendus SQLite pour les violations de contrainte.
const (
	sqliteConstraint           = 19
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
	sqliteConstraintForeignKey = 787
	sqliteConstraintCheck      = 275
	sqliteConstraintNotNull    = 1299
	sqliteConstraintTrigger    = 1811
)

// traduire convertit une erreur SQLite de contrainte en erreur sentinelle du
// paquet, en conservant le message d'origine comme contexte. Les autres
// erreurs (et nil) sont renvoyées telles quelles.
func traduire(contexte string, err error) error {
	if err == nil {
		return nil
	}

	var e *sqlite.Error
	if errors.As(err, &e) {
		switch code := e.Code(); {
		case code == sqliteConstraintUnique || code == sqliteConstraintPrimaryKey:
			return fmt.Errorf("%s : %w (%s)", contexte, ErrConflit, e.Error())
		case code == sqliteConstraintForeignKey:
			return fmt.Errorf("%s : %w (clé étrangère)", contexte, ErrReference)
		case code == sqliteConstraintNotNull || code == sqliteConstraintCheck:
			return fmt.Errorf("%s : %w (%s)", contexte, ErrValidation, e.Error())
		case code == sqliteConstraintTrigger:
			return fmt.Errorf("%s : %w (%s)", contexte, ErrInvariant, e.Error())
		case code == sqliteConstraint || code/256 == sqliteConstraint:
			return fmt.Errorf("%s : %w (%s)", contexte, ErrValidation, e.Error())
		}
	}

	// Repli sur le texte quand le pilote n'expose pas le code attendu.
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE constraint failed"):
		return fmt.Errorf("%s : %w (%s)", contexte, ErrConflit, msg)
	case strings.Contains(msg, "FOREIGN KEY constraint failed"):
		return fmt.Errorf("%s : %w (clé étrangère)", contexte, ErrReference)
	case strings.Contains(msg, "constraint failed"):
		return fmt.Errorf("%s : %w (%s)", contexte, ErrValidation, msg)
	}
	return fmt.Errorf("%s : %w", contexte, err)
}
