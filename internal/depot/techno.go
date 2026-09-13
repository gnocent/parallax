package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Techno est un référentiel simple, calqué sur Projet : un code stable, un
// libellé, un drapeau d'activité. Recense les technologies suivies
// (Elasticsearch, Kafka…).
type Techno struct {
	ID      int64
	Code    string
	Libelle string
	Actif   bool
}

// CreerTechno insère une techno et renvoie la ligne complète, identifiant
// attribué. La techno est active par défaut.
//
// Erreurs : ErrValidation si code ou libellé vide ; ErrConflit si le code est
// déjà pris.
func (d *Depot) CreerTechno(techno Techno) (Techno, error) {
	techno.Code = strings.TrimSpace(techno.Code)
	techno.Libelle = strings.TrimSpace(techno.Libelle)
	if techno.Code == "" {
		return Techno{}, fmt.Errorf("création d'une techno : %w : code vide", ErrValidation)
	}
	if techno.Libelle == "" {
		return Techno{}, fmt.Errorf("création d'une techno : %w : libellé vide", ErrValidation)
	}

	return creerJournalise(d, "techno", func(tx *sql.Tx) (Techno, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO techno (code, libelle) VALUES (?, ?)`, techno.Code, techno.Libelle)
		if err != nil {
			return Techno{}, 0, traduire("création de la techno "+techno.Code, err)
		}
		techno.ID, _ = res.LastInsertId()
		techno.Actif = true
		return techno, techno.ID, nil
	})
}

// LireTechno renvoie une techno par identifiant, ou ErrIntrouvable.
func (d *Depot) LireTechno(id int64) (Techno, error) {
	return lireTechno(d.base, id)
}

// LireTechnoParCode renvoie une techno par son code, ou ErrIntrouvable.
func (d *Depot) LireTechnoParCode(code string) (Techno, error) {
	var techno Techno
	err := scanUn(
		d.base.QueryRow(`SELECT id, code, libelle, actif FROM techno WHERE code = ?`,
			strings.TrimSpace(code)),
		"lecture de la techno "+code,
		&techno.ID, &techno.Code, &techno.Libelle, &techno.Actif)
	return techno, err
}

// ListerTechnos renvoie les technos triées par code. Les technos désactivées
// sont exclues sauf si inclureInactifs est vrai.
func (d *Depot) ListerTechnos(inclureInactifs bool) ([]Techno, error) {
	requete := `SELECT id, code, libelle, actif FROM techno`
	if !inclureInactifs {
		requete += ` WHERE actif = 1`
	}
	requete += ` ORDER BY code`

	lignes, err := d.base.Query(requete)
	if err != nil {
		return nil, traduire("liste des technos", err)
	}
	defer lignes.Close()

	var out []Techno
	for lignes.Next() {
		var techno Techno
		if err := lignes.Scan(&techno.ID, &techno.Code, &techno.Libelle, &techno.Actif); err != nil {
			return nil, fmt.Errorf("liste des technos : %w", err)
		}
		out = append(out, techno)
	}
	return out, lignes.Err()
}

// ModifierTechno met à jour le code et le libellé. L'activité se pilote par
// ArchiverTechno / ReactiverTechno, transitions volontairement distinctes.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation si
// code ou libellé vide ; ErrConflit si le nouveau code est déjà pris.
func (d *Depot) ModifierTechno(techno Techno) error {
	techno.Code = strings.TrimSpace(techno.Code)
	techno.Libelle = strings.TrimSpace(techno.Libelle)
	if techno.Code == "" || techno.Libelle == "" {
		return fmt.Errorf("modification de la techno %d : %w : code ou libellé vide",
			techno.ID, ErrValidation)
	}

	return modifierJournalise(d, "techno", techno.ID, ActionModification, lireTechno, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE techno SET code = ?, libelle = ? WHERE id = ?`,
			techno.Code, techno.Libelle, techno.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de la techno %d", techno.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification de la techno %d", techno.ID))
	})
}

// ArchiverTechno désactive une techno sans la supprimer : elle disparaît des
// listes courantes mais les clusters, règles et variables qui la référencent
// restent lisibles. Rien n'est jamais détruit.
func (d *Depot) ArchiverTechno(id int64) error {
	return modifierJournalise(d, "techno", id, ActionModification, lireTechno, func(tx *sql.Tx) error {
		return basculerActif(tx, "techno", id, false)
	})
}

// ReactiverTechno réactive une techno archivée.
func (d *Depot) ReactiverTechno(id int64) error {
	return modifierJournalise(d, "techno", id, ActionModification, lireTechno, func(tx *sql.Tx) error {
		return basculerActif(tx, "techno", id, true)
	})
}

// ---------------------------------------------------------------- helpers

func lireTechno(c conn, id int64) (Techno, error) {
	var techno Techno
	err := scanUn(
		c.QueryRow(`SELECT id, code, libelle, actif FROM techno WHERE id = ?`, id),
		fmt.Sprintf("lecture de la techno %d", id),
		&techno.ID, &techno.Code, &techno.Libelle, &techno.Actif)
	return techno, err
}
