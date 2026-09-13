package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Vue est un tableau croisé enregistré : la configuration du constructeur de
// vues (axes, filtres, colonnes), sérialisée en JSON par l'appelant — le
// dépôt ne connaît pas la structure de ce JSON, c'est l'affaire du paquet
// internal/vues. Équivalent des tableaux croisés dynamiques actuels.
type Vue struct {
	ID             int64
	Nom            string
	ProprietaireID *int64
	Partagee       bool
	Axes           string // JSON
	Filtres        string // JSON
	Colonnes       string // JSON
	DateCreation   string
}

// CreerVue enregistre une vue. nom obligatoire ; axes/filtres/colonnes sont
// stockés tels quels (JSON déjà sérialisé par l'appelant).
func (d *Depot) CreerVue(v Vue) (Vue, error) {
	v.Nom = strings.TrimSpace(v.Nom)
	if v.Nom == "" {
		return Vue{}, fmt.Errorf("création d'une vue : %w : nom vide", ErrValidation)
	}
	v.DateCreation = Horodatage()

	err := d.enTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO vue (nom, proprietaire, partagee, axes, filtres, colonnes, date_creation)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			v.Nom, v.ProprietaireID, boolInt(v.Partagee), v.Axes, v.Filtres, v.Colonnes, v.DateCreation)
		if err != nil {
			return traduire("création de la vue "+v.Nom, err)
		}
		v.ID, _ = res.LastInsertId()
		return d.journalCreationTable(tx, "vue", "vue", v.ID)
	})
	if err != nil {
		return Vue{}, err
	}
	return v, nil
}

// LireVue renvoie une vue par identifiant, ou ErrIntrouvable.
func (d *Depot) LireVue(id int64) (Vue, error) {
	var v Vue
	err := scanUn(
		d.base.QueryRow(
			`SELECT id, nom, proprietaire, partagee, axes, filtres, colonnes, date_creation
			 FROM vue WHERE id = ?`, id),
		fmt.Sprintf("lecture de la vue %d", id),
		&v.ID, &v.Nom, &v.ProprietaireID, &v.Partagee, &v.Axes, &v.Filtres, &v.Colonnes, &v.DateCreation)
	return v, err
}

// ListerVuesAccessibles renvoie les vues qu'un utilisateur peut voir : les
// siennes, plus celles que d'autres ont partagées. Triées par nom.
func (d *Depot) ListerVuesAccessibles(utilisateurID int64) ([]Vue, error) {
	lignes, err := d.base.Query(
		`SELECT id, nom, proprietaire, partagee, axes, filtres, colonnes, date_creation
		 FROM vue WHERE partagee = 1 OR proprietaire = ?
		 ORDER BY nom`, utilisateurID)
	if err != nil {
		return nil, traduire("liste des vues", err)
	}
	defer lignes.Close()

	var out []Vue
	for lignes.Next() {
		var v Vue
		if err := lignes.Scan(&v.ID, &v.Nom, &v.ProprietaireID, &v.Partagee,
			&v.Axes, &v.Filtres, &v.Colonnes, &v.DateCreation); err != nil {
			return nil, fmt.Errorf("liste des vues : %w", err)
		}
		out = append(out, v)
	}
	return out, lignes.Err()
}

// ModifierVue met à jour le nom, le partage et la configuration d'une vue.
func (d *Depot) ModifierVue(v Vue) error {
	v.Nom = strings.TrimSpace(v.Nom)
	if v.Nom == "" {
		return fmt.Errorf("modification de la vue %d : %w : nom vide", v.ID, ErrValidation)
	}
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "vue", v.ID)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE vue SET nom = ?, partagee = ?, axes = ?, filtres = ?, colonnes = ? WHERE id = ?`,
			v.Nom, boolInt(v.Partagee), v.Axes, v.Filtres, v.Colonnes, v.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de la vue %d", v.ID), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("modification de la vue %d", v.ID)); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "vue", "vue", v.ID, ActionModification, avant)
	})
}

// SupprimerVue retire une vue enregistrée. Une vue est un raccourci
// d'affichage, pas une donnée du réel : la supprimer n'efface aucune donnée
// métier, seulement la configuration de regroupement.
func (d *Depot) SupprimerVue(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "vue", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM vue WHERE id = ?`, id)
		if err != nil {
			return traduire(fmt.Sprintf("suppression de la vue %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("suppression de la vue %d", id)); err != nil {
			return err
		}
		return d.journalSuppressionTable(tx, "vue", id, avant)
	})
}
