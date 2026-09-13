package depot

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Clés de la table parametre (docs/modele-donnees.md §10.1). Une constante par
// clé connue de l'application : la table est un simple magasin clé → valeur,
// mais aucun écran ne doit inventer sa clé en littéral.
const (
	// ParametreGabaritDemande est le gabarit global de description des
	// demandes de matériel (backlog v3.2), texte à variables {nom}.
	ParametreGabaritDemande = "gabarit_demande"

	// Sauvegarde locale automatique (backlog v3.7) : dossier, heure
	// quotidienne ("HH:MM") et rétention en jours, réglés depuis
	// /parametres/sauvegarde ; absents ou vides, la fonctionnalité est
	// désactivée — jamais de dossier par défaut choisi à la place de
	// l'administrateur. Les deux dernières clés sont un état interne écrit
	// par le planificateur (cmd/parallax), pas saisies à l'écran.
	ParametreSauvegardeDossier              = "sauvegarde_dossier"
	ParametreSauvegardeHeure                = "sauvegarde_heure"
	ParametreSauvegardeRetentionJours       = "sauvegarde_retention_jours"
	ParametreSauvegardeDerniereReussite     = "sauvegarde_derniere_reussite"
	ParametreSauvegardeDerniereVerification = "sauvegarde_derniere_verification"
)

// LireParametre renvoie la valeur d'un paramètre d'application. present vaut
// faux si la clé n'a jamais été définie — l'appelant décide alors du défaut,
// le dépôt ne le connaît pas (un gabarit absent n'est pas un gabarit vide :
// l'écran des demandes l'annonce et renvoie vers son édition).
func (d *Depot) LireParametre(cle string) (valeur string, present bool, err error) {
	cle = strings.TrimSpace(cle)
	if cle == "" {
		return "", false, fmt.Errorf("lecture d'un paramètre : %w : clé vide", ErrValidation)
	}
	err = d.base.QueryRow(`SELECT valeur FROM parametre WHERE cle = ?`, cle).Scan(&valeur)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("lecture du paramètre %s : %w", cle, err)
	}
	return valeur, true, nil
}

// DefinirParametre crée ou remplace un paramètre d'application (upsert) en
// posant l'horodatage de modification et l'auteur. Une valeur vide est
// admise : c'est à l'écran de décider si « vide » a un sens pour sa clé.
//
// Erreurs : ErrValidation si la clé est vide ; ErrReference si l'utilisateur
// n'existe pas.
func (d *Depot) DefinirParametre(cle, valeur string, utilisateurID *int64) error {
	cle = strings.TrimSpace(cle)
	if cle == "" {
		return fmt.Errorf("définition d'un paramètre : %w : clé vide", ErrValidation)
	}
	return d.enTx(func(tx *sql.Tx) error {
		// journal : la clé est textuelle, entite_id vaut 0 et la clé voyage
		// dans les états avant/après.
		var avant map[string]any
		var ancienne string
		switch err := tx.QueryRow(`SELECT valeur FROM parametre WHERE cle = ?`, cle).Scan(&ancienne); {
		case err == nil:
			avant = map[string]any{"cle": cle, "valeur": ancienne}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return fmt.Errorf("lecture du paramètre %s : %w", cle, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO parametre (cle, valeur, modifie_le, modifie_par)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT (cle) DO UPDATE SET
			   valeur = excluded.valeur,
			   modifie_le = excluded.modifie_le,
			   modifie_par = excluded.modifie_par`,
			cle, valeur, Horodatage(), utilisateurID); err != nil {
			return traduire("définition du paramètre "+cle, err)
		}
		action := ActionModification
		if avant == nil {
			action = ActionCreation
		}
		return d.journaliser(tx, "parametre", 0, action, avant, map[string]any{"cle": cle, "valeur": valeur})
	})
}
