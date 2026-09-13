package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Projet est un référentiel simple : un code stable, un libellé, un drapeau
// d'activité. Sert de patron aux autres dépôts de référentiel (environnement,
// techno, tier, usage_fonctionnel, zone).
type Projet struct {
	ID      int64
	Code    string
	Libelle string
	Actif   bool
}

// CreerProjet insère un projet et renvoie la ligne complète, identifiant
// attribué. Le projet est actif par défaut.
//
// Erreurs : ErrValidation si code ou libellé vide ; ErrConflit si le code
// est déjà pris.
func (d *Depot) CreerProjet(p Projet) (Projet, error) {
	p.Code = strings.TrimSpace(p.Code)
	p.Libelle = strings.TrimSpace(p.Libelle)
	if p.Code == "" {
		return Projet{}, fmt.Errorf("création d'un projet : %w : code vide", ErrValidation)
	}
	if p.Libelle == "" {
		return Projet{}, fmt.Errorf("création d'un projet : %w : libellé vide", ErrValidation)
	}

	return creerJournalise(d, "projet", func(tx *sql.Tx) (Projet, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO projet (code, libelle) VALUES (?, ?)`, p.Code, p.Libelle)
		if err != nil {
			return Projet{}, 0, traduire("création du projet "+p.Code, err)
		}
		p.ID, _ = res.LastInsertId()
		p.Actif = true
		return p, p.ID, nil
	})
}

// LireProjet renvoie un projet par identifiant, ou ErrIntrouvable.
func (d *Depot) LireProjet(id int64) (Projet, error) {
	return lireProjet(d.base, id)
}

// LireProjetParCode renvoie un projet par son code, ou ErrIntrouvable.
func (d *Depot) LireProjetParCode(code string) (Projet, error) {
	var p Projet
	err := scanUn(
		d.base.QueryRow(`SELECT id, code, libelle, actif FROM projet WHERE code = ?`,
			strings.TrimSpace(code)),
		"lecture du projet "+code,
		&p.ID, &p.Code, &p.Libelle, &p.Actif)
	return p, err
}

// ListerProjets renvoie les projets triés par code. Les projets désactivés
// sont exclus sauf si inclureInactifs est vrai.
func (d *Depot) ListerProjets(inclureInactifs bool) ([]Projet, error) {
	requete := `SELECT id, code, libelle, actif FROM projet`
	if !inclureInactifs {
		requete += ` WHERE actif = 1`
	}
	requete += ` ORDER BY code`

	lignes, err := d.base.Query(requete)
	if err != nil {
		return nil, traduire("liste des projets", err)
	}
	defer lignes.Close()

	var out []Projet
	for lignes.Next() {
		var p Projet
		if err := lignes.Scan(&p.ID, &p.Code, &p.Libelle, &p.Actif); err != nil {
			return nil, fmt.Errorf("liste des projets : %w", err)
		}
		out = append(out, p)
	}
	return out, lignes.Err()
}

// ModifierProjet met à jour le code et le libellé. L'activité se pilote par
// ArchiverProjet / ReactiverProjet, transitions volontairement distinctes.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation si
// code ou libellé vide ; ErrConflit si le nouveau code est déjà pris.
func (d *Depot) ModifierProjet(p Projet) error {
	p.Code = strings.TrimSpace(p.Code)
	p.Libelle = strings.TrimSpace(p.Libelle)
	if p.Code == "" || p.Libelle == "" {
		return fmt.Errorf("modification du projet %d : %w : code ou libellé vide",
			p.ID, ErrValidation)
	}

	return modifierJournalise(d, "projet", p.ID, ActionModification, lireProjet, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE projet SET code = ?, libelle = ? WHERE id = ?`,
			p.Code, p.Libelle, p.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification du projet %d", p.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification du projet %d", p.ID))
	})
}

// ArchiverProjet désactive un projet sans le supprimer : il disparaît des
// listes courantes mais les clusters, scénarios et règles qui le référencent
// restent lisibles. Rien n'est jamais détruit.
func (d *Depot) ArchiverProjet(id int64) error {
	return modifierJournalise(d, "projet", id, ActionModification, lireProjet, func(tx *sql.Tx) error {
		return basculerActif(tx, "projet", id, false)
	})
}

// ReactiverProjet réactive un projet archivé.
func (d *Depot) ReactiverProjet(id int64) error {
	return modifierJournalise(d, "projet", id, ActionModification, lireProjet, func(tx *sql.Tx) error {
		return basculerActif(tx, "projet", id, true)
	})
}

// ---------------------------------------------------------------- helpers

func lireProjet(c conn, id int64) (Projet, error) {
	var p Projet
	err := scanUn(
		c.QueryRow(`SELECT id, code, libelle, actif FROM projet WHERE id = ?`, id),
		fmt.Sprintf("lecture du projet %d", id),
		&p.ID, &p.Code, &p.Libelle, &p.Actif)
	return p, err
}
