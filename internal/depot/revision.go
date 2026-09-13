package depot

import (
	"database/sql"
	"fmt"
)

// Revision est une variante datée et immuable d'un modèle. Toute évolution
// matérielle réelle (ajout de disques, changement de carte) crée une nouvelle
// révision ; on ne modifie jamais une révision existante en place, sauf à
// corriger explicitement une erreur de saisie.
//
// Mapping IHM — deux actions, deux libellés distincts :
//   - « Créer une révision »   → CreerRevision
//   - « Corriger une erreur »  → ModifierRevision(r, correction=true)
//
// C'est cette immuabilité qui donne la vision historique du parc sans
// bitemporalité : une révision ancienne conserve les caractéristiques de son
// époque (invariant 1 du §12 de docs/modele-donnees.md).
type Revision struct {
	ID          int64
	ModeleID    int64
	Numero      int64
	Libelle     *string
	DateEffet   string // ISO YYYY-MM-DD
	Commentaire *string
}

// CreerRevision insère une révision pour un modèle. Le numéro est attribué
// automatiquement — COALESCE(MAX(numero), 0) + 1 pour ce modèle — dans une
// transaction, pour que deux créations concurrentes ne se voient pas attribuer
// le même numéro.
//
// Erreurs : ErrValidation si date_effet est mal formée ; ErrReference si le
// modèle n'existe pas.
func (d *Depot) CreerRevision(r Revision) (Revision, error) {
	if err := ValiderDate("date_effet", r.DateEffet); err != nil {
		return Revision{}, err
	}

	err := d.enTx(func(tx *sql.Tx) error {
		var numero int64
		if err := tx.QueryRow(
			`SELECT COALESCE(MAX(numero), 0) + 1 FROM revision WHERE modele_id = ?`,
			r.ModeleID).Scan(&numero); err != nil {
			return traduire(fmt.Sprintf("numérotation d'une révision du modèle %d", r.ModeleID), err)
		}

		res, err := tx.Exec(
			`INSERT INTO revision (modele_id, numero, libelle, date_effet, commentaire)
			 VALUES (?, ?, ?, ?, ?)`,
			r.ModeleID, numero, r.Libelle, r.DateEffet, r.Commentaire)
		if err != nil {
			return traduire(fmt.Sprintf("création d'une révision du modèle %d", r.ModeleID), err)
		}
		r.ID, _ = res.LastInsertId()
		r.Numero = numero
		return d.journaliser(tx, "revision", r.ID, ActionCreation, nil, r)
	})
	if err != nil {
		return Revision{}, err
	}
	return r, nil
}

// LireRevision renvoie une révision par identifiant, ou ErrIntrouvable.
func (d *Depot) LireRevision(id int64) (Revision, error) {
	return lireRevision(d.base, id)
}

// ListerRevisions renvoie les révisions d'un modèle, triées par numéro
// croissant (donc chronologiquement).
func (d *Depot) ListerRevisions(modeleID int64) ([]Revision, error) {
	lignes, err := d.base.Query(
		`SELECT id, modele_id, numero, libelle, date_effet, commentaire
		 FROM revision WHERE modele_id = ? ORDER BY numero`, modeleID)
	if err != nil {
		return nil, traduire(fmt.Sprintf("liste des révisions du modèle %d", modeleID), err)
	}
	defer lignes.Close()

	var out []Revision
	for lignes.Next() {
		var r Revision
		if err := lignes.Scan(&r.ID, &r.ModeleID, &r.Numero, &r.Libelle,
			&r.DateEffet, &r.Commentaire); err != nil {
			return nil, fmt.Errorf("liste des révisions du modèle %d : %w", modeleID, err)
		}
		out = append(out, r)
	}
	return out, lignes.Err()
}

// RevisionEstReferencee indique si au moins un serveur_revision pointe sur
// cette révision. C'est le déclencheur de l'immuabilité : une révision
// référencée ne se modifie plus qu'en mode correction d'erreur.
func (d *Depot) RevisionEstReferencee(id int64) (bool, error) {
	return revisionEstReferencee(d.base, id)
}

// ModifierRevision met à jour les champs modifiables d'une révision (libellé,
// date d'effet, commentaire).
//
// Invariant 1 — immuabilité :
//   - correction == false : si la révision est déjà référencée par un
//     serveur_revision, rien n'est modifié et ErrImmuable est renvoyée.
//     L'appelant doit alors créer une nouvelle révision.
//   - correction == true : la modification est appliquée même si la révision
//     est référencée. Le retour avertissement vaut true dans ce cas, pour que
//     l'appelant vérifie que l'IHM a bien affiché son avertissement avant
//     d'écraser un historique.
//
// Erreurs : ErrValidation si date_effet mal formée ; ErrIntrouvable si la
// révision n'existe pas ; ErrImmuable comme décrit ci-dessus.
func (d *Depot) ModifierRevision(r Revision, correction bool) (avertissement bool, err error) {
	if err := ValiderDate("date_effet", r.DateEffet); err != nil {
		return false, err
	}

	err = d.enTx(func(tx *sql.Tx) error {
		reference, err := revisionEstReferencee(tx, r.ID)
		if err != nil {
			return err
		}
		if reference && !correction {
			return fmt.Errorf("modification de la révision %d : %w", r.ID, ErrImmuable)
		}
		avertissement = reference

		avant, err := lireRevision(tx, r.ID)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE revision SET libelle = ?, date_effet = ?, commentaire = ?
			 WHERE id = ?`,
			r.Libelle, r.DateEffet, r.Commentaire, r.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de la révision %d", r.ID), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("modification de la révision %d", r.ID)); err != nil {
			return err
		}
		apres, err := lireRevision(tx, r.ID)
		if err != nil {
			return err
		}
		// écraser une révision référencée est LE geste que le journal doit
		// rendre visible : CORRECTION plutôt que MODIFICATION.
		return d.journaliser(tx, "revision", r.ID, actionCatalogue(reference, ActionModification), avant, apres)
	})
	if err != nil {
		return false, err
	}
	return avertissement, nil
}

// ---------------------------------------------------------------- helpers

func lireRevision(c conn, id int64) (Revision, error) {
	var r Revision
	err := scanUn(
		c.QueryRow(
			`SELECT id, modele_id, numero, libelle, date_effet, commentaire
			 FROM revision WHERE id = ?`, id),
		fmt.Sprintf("lecture de la révision %d", id),
		&r.ID, &r.ModeleID, &r.Numero, &r.Libelle, &r.DateEffet, &r.Commentaire)
	return r, err
}

func revisionEstReferencee(c conn, id int64) (bool, error) {
	var n int64
	if err := c.QueryRow(
		`SELECT COUNT(*) FROM serveur_revision WHERE revision_id = ?`, id).Scan(&n); err != nil {
		return false, traduire(fmt.Sprintf("vérification des références de la révision %d", id), err)
	}
	return n > 0, nil
}

// garantirRevisionModifiable est le garde d'immuabilité partagé par les dépôts
// composant et modele_noeud : hors mode correction, une révision référencée
// par un serveur_revision refuse toute retouche de sa composition.
func garantirRevisionModifiable(c conn, revisionID int64, correction bool) error {
	if correction {
		return nil
	}
	reference, err := revisionEstReferencee(c, revisionID)
	if err != nil {
		return err
	}
	if reference {
		return fmt.Errorf("révision %d : %w", revisionID, ErrImmuable)
	}
	return nil
}
