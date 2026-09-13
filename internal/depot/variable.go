package depot

import (
	"database/sql"
	"fmt"
	"strings"

	"parallax/internal/capacity"
)

// Variable est un paramètre de calcul déclaré. Ses valeurs sont indexées par
// (scénario, année) et par portée hiérarchique dans variable_valeur.
type Variable struct {
	ID          int64
	Code        string
	Libelle     string
	Unite       *string
	Defaut      *float64
	Commentaire *string
}

// PorteeVariable situe une valeur dans la hiérarchie global → projet →
// environnement → techno → tier → cluster. Tout pointeur nil signifie « ne
// restreint pas à ce niveau ».
type PorteeVariable struct {
	ProjetID        *int64
	EnvironnementID *int64
	TechnoID        *int64
	TierID          *int64
	ClusterID       *int64
}

// ValeurVariable est une valeur saisie, avec son scénario, son année et sa
// portée.
type ValeurVariable struct {
	ID          int64
	VariableID  int64
	ScenarioID  *int64
	Annee       int
	Portee      PorteeVariable
	Valeur      float64
	Commentaire *string
	ModifieLe   string
	ModifiePar  *int64
}

// EntreeHistoriqueValeur trace un écrasement de valeur, garde-fou contre la
// modification accidentelle d'un facteur empirique.
type EntreeHistoriqueValeur struct {
	ID            int64
	ValeurID      int64
	Ancienne      float64
	Nouvelle      float64
	Horodatage    string
	UtilisateurID *int64
}

// ------------------------------------------------------------- déclaration

// CreerVariable insère une variable. code et libellé obligatoires.
func (d *Depot) CreerVariable(v Variable) (Variable, error) {
	v.Code = strings.TrimSpace(v.Code)
	v.Libelle = strings.TrimSpace(v.Libelle)
	if v.Code == "" || v.Libelle == "" {
		return Variable{}, fmt.Errorf(
			"création d'une variable : %w : code et libellé obligatoires", ErrValidation)
	}
	err := d.enTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO variable (code, libelle, unite, defaut, commentaire)
			 VALUES (?, ?, ?, ?, ?)`,
			v.Code, v.Libelle, v.Unite, v.Defaut, v.Commentaire)
		if err != nil {
			return traduire("création de la variable "+v.Code, err)
		}
		v.ID, _ = res.LastInsertId()
		return d.journalCreationTable(tx, "variable", "variable", v.ID)
	})
	if err != nil {
		return Variable{}, err
	}
	return v, nil
}

// LireVariable renvoie une variable par identifiant, ou ErrIntrouvable.
func (d *Depot) LireVariable(id int64) (Variable, error) {
	return lireVariable(d.base, "id = ?", id)
}

// LireVariableParCode renvoie une variable par son code, ou ErrIntrouvable.
func (d *Depot) LireVariableParCode(code string) (Variable, error) {
	return lireVariable(d.base, "code = ?", strings.TrimSpace(code))
}

// ListerVariables renvoie toutes les variables, triées par code.
func (d *Depot) ListerVariables() ([]Variable, error) {
	lignes, err := d.base.Query(
		`SELECT id, code, libelle, unite, defaut, commentaire FROM variable ORDER BY code`)
	if err != nil {
		return nil, traduire("liste des variables", err)
	}
	defer lignes.Close()

	var out []Variable
	for lignes.Next() {
		var v Variable
		if err := lignes.Scan(&v.ID, &v.Code, &v.Libelle, &v.Unite, &v.Defaut, &v.Commentaire); err != nil {
			return nil, fmt.Errorf("liste des variables : %w", err)
		}
		out = append(out, v)
	}
	return out, lignes.Err()
}

// ModifierVariable met à jour le libellé, l'unité, la valeur par défaut et le
// commentaire. Le code reste stable.
func (d *Depot) ModifierVariable(v Variable) error {
	v.Libelle = strings.TrimSpace(v.Libelle)
	if v.Libelle == "" {
		return fmt.Errorf("modification de la variable %d : %w : libellé vide", v.ID, ErrValidation)
	}
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "variable", v.ID)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE variable SET libelle = ?, unite = ?, defaut = ?, commentaire = ? WHERE id = ?`,
			v.Libelle, v.Unite, v.Defaut, v.Commentaire, v.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de la variable %d", v.ID), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("modification de la variable %d", v.ID)); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "variable", "variable", v.ID, ActionModification, avant)
	})
}

// ------------------------------------------------------------------ valeurs

// DefinirValeur pose ou remplace la valeur d'une variable pour un scénario,
// une année et une portée donnés. Si une valeur existait déjà à cette portée
// exacte et qu'elle change, l'ancienne est consignée dans l'historique avant
// écrasement.
//
// Erreurs : ErrValidation si l'année est absurde ; ErrReference si la variable
// ou une portée n'existe pas.
func (d *Depot) DefinirValeur(variableID int64, scenarioID *int64, annee int,
	p PorteeVariable, valeur float64, commentaire *string, utilisateurID *int64) (ValeurVariable, error) {

	if annee < 2000 || annee > 2100 {
		return ValeurVariable{}, fmt.Errorf(
			"valeur de variable : %w : année %d hors plage", ErrValidation, annee)
	}

	var enregistree ValeurVariable
	err := d.enTx(func(tx *sql.Tx) error {
		clause, args := scopeWhereValeur(variableID, scenarioID, annee, p)

		var (
			existeID int64
			ancienne float64
		)
		row := tx.QueryRow(
			`SELECT id, valeur FROM variable_valeur WHERE `+clause, args...)
		switch err := row.Scan(&existeID, &ancienne); err {
		case nil:
			if ancienne != valeur {
				if _, err := tx.Exec(
					`INSERT INTO variable_valeur_historique
					 (valeur_id, ancienne, nouvelle, horodatage, utilisateur_id)
					 VALUES (?, ?, ?, ?, ?)`,
					existeID, ancienne, valeur, Horodatage(), utilisateurID); err != nil {
					return traduire("historisation de la valeur", err)
				}
			}
			avant, err := instantane(tx, "variable_valeur", existeID)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(
				`UPDATE variable_valeur
				 SET valeur = ?, commentaire = ?, modifie_le = ?, modifie_par = ?
				 WHERE id = ?`,
				valeur, commentaire, Horodatage(), utilisateurID, existeID); err != nil {
				return traduire("mise à jour de la valeur", err)
			}
			enregistree.ID = existeID
			return d.journalModificationTable(tx, "variable_valeur", "variable_valeur", existeID, ActionModification, avant)
		case sql.ErrNoRows:
			res, err := tx.Exec(
				`INSERT INTO variable_valeur
				 (variable_id, scenario_id, annee, projet_id, environnement_id,
				  techno_id, tier_id, cluster_id, valeur, commentaire, modifie_le, modifie_par)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				variableID, scenarioID, annee,
				p.ProjetID, p.EnvironnementID, p.TechnoID, p.TierID, p.ClusterID,
				valeur, commentaire, Horodatage(), utilisateurID)
			if err != nil {
				return traduire("création de la valeur", err)
			}
			enregistree.ID, _ = res.LastInsertId()
			return d.journalCreationTable(tx, "variable_valeur", "variable_valeur", enregistree.ID)
		default:
			return fmt.Errorf("valeur de variable : %w", err)
		}
	})
	if err != nil {
		return ValeurVariable{}, err
	}
	return d.LireValeurParID(enregistree.ID)
}

// LireValeurParID renvoie une valeur par identifiant.
func (d *Depot) LireValeurParID(id int64) (ValeurVariable, error) {
	return scanValeur(d.base.QueryRow(sqlSelectValeur+` WHERE id = ?`, id),
		fmt.Sprintf("lecture de la valeur %d", id))
}

// ListerValeurs renvoie les valeurs saisies d'une variable, éventuellement
// filtrées par scénario et par année. Le réel (scenario_id NULL) est toujours
// inclus ; passer scenarioID non nil ajoute les surcharges de ce scénario.
func (d *Depot) ListerValeurs(variableID int64, scenarioID *int64, annee *int) ([]ValeurVariable, error) {
	requete := sqlSelectValeur + ` WHERE variable_id = ? AND (scenario_id IS NULL`
	args := []any{variableID}
	if scenarioID != nil {
		requete += ` OR scenario_id = ?`
		args = append(args, *scenarioID)
	}
	requete += `)`
	if annee != nil {
		requete += ` AND annee = ?`
		args = append(args, *annee)
	}
	requete += ` ORDER BY annee, (scenario_id IS NOT NULL), scenario_id,
		cluster_id, tier_id, techno_id, environnement_id, projet_id`

	lignes, err := d.base.Query(requete, args...)
	if err != nil {
		return nil, traduire("liste des valeurs", err)
	}
	defer lignes.Close()

	var out []ValeurVariable
	for lignes.Next() {
		v, err := scanValeurLignes(lignes)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, lignes.Err()
}

// SupprimerValeur retire une valeur saisie. C'est une opération d'édition
// normale : une valeur de variable est un paramètre, pas une donnée du réel à
// préserver. L'historique des écrasements antérieurs reste consultable.
func (d *Depot) SupprimerValeur(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "variable_valeur", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM variable_valeur WHERE id = ?`, id)
		if err != nil {
			return traduire(fmt.Sprintf("suppression de la valeur %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("suppression de la valeur %d", id)); err != nil {
			return err
		}
		return d.journalSuppressionTable(tx, "variable_valeur", id, avant)
	})
}

// HistoriqueValeur renvoie les écrasements successifs d'une valeur, du plus
// récent au plus ancien.
func (d *Depot) HistoriqueValeur(valeurID int64) ([]EntreeHistoriqueValeur, error) {
	lignes, err := d.base.Query(
		`SELECT id, valeur_id, ancienne, nouvelle, horodatage, utilisateur_id
		 FROM variable_valeur_historique WHERE valeur_id = ?
		 ORDER BY horodatage DESC, id DESC`, valeurID)
	if err != nil {
		return nil, traduire("historique de la valeur", err)
	}
	defer lignes.Close()

	var out []EntreeHistoriqueValeur
	for lignes.Next() {
		var e EntreeHistoriqueValeur
		if err := lignes.Scan(&e.ID, &e.ValeurID, &e.Ancienne, &e.Nouvelle,
			&e.Horodatage, &e.UtilisateurID); err != nil {
			return nil, fmt.Errorf("historique de la valeur : %w", err)
		}
		out = append(out, e)
	}
	return out, lignes.Err()
}

// ResoudreValeur applique la règle de résolution du §7 : scénario avant réel,
// puis portée la plus spécifique, puis valeur par défaut de la variable. La
// sémantique exacte est celle de capacity.ResoudreVariable, couverte par les
// vecteurs de la spécification.
//
// Le second retour est false uniquement si aucune valeur ne s'applique et que
// la variable n'a pas de défaut.
func (d *Depot) ResoudreValeur(code string, c capacity.Cluster, annee int,
	scenarioID *int64) (float64, bool, error) {

	v, err := d.LireVariableParCode(code)
	if err != nil {
		return 0, false, err
	}

	lignes, err := d.base.Query(
		`SELECT scenario_id, annee, projet_id, environnement_id, techno_id, tier_id,
		        cluster_id, valeur
		 FROM variable_valeur WHERE variable_id = ? AND annee = ?`, v.ID, annee)
	if err != nil {
		return 0, false, traduire("résolution de "+code, err)
	}
	defer lignes.Close()

	var valeurs []capacity.ValeurVariable
	for lignes.Next() {
		vv := capacity.ValeurVariable{Code: code, Annee: annee}
		if err := lignes.Scan(&vv.ScenarioID, &vv.Annee, &vv.ProjetID, &vv.EnvironnementID,
			&vv.TechnoID, &vv.TierID, &vv.ClusterID, &vv.Valeur); err != nil {
			return 0, false, fmt.Errorf("résolution de %s : %w", code, err)
		}
		valeurs = append(valeurs, vv)
	}
	if err := lignes.Err(); err != nil {
		return 0, false, err
	}

	if r, ok := capacity.ResoudreVariable(valeurs, code, c, annee, scenarioID); ok {
		return r, true, nil
	}
	if v.Defaut != nil {
		return *v.Defaut, true, nil
	}
	return 0, false, nil
}

// --------------------------------------------------------------- helpers

const sqlSelectValeur = `SELECT id, variable_id, scenario_id, annee, projet_id,
	environnement_id, techno_id, tier_id, cluster_id, valeur, commentaire,
	modifie_le, modifie_par FROM variable_valeur`

func lireVariable(c conn, ou string, arg any) (Variable, error) {
	var v Variable
	err := scanUn(
		c.QueryRow(`SELECT id, code, libelle, unite, defaut, commentaire
		            FROM variable WHERE `+ou, arg),
		"lecture d'une variable",
		&v.ID, &v.Code, &v.Libelle, &v.Unite, &v.Defaut, &v.Commentaire)
	return v, err
}

func scopeWhereValeur(variableID int64, scenarioID *int64, annee int, p PorteeVariable) (string, []any) {
	return `variable_id = ? AND annee = ?
		AND COALESCE(scenario_id, -1)      = COALESCE(?, -1)
		AND COALESCE(projet_id, -1)        = COALESCE(?, -1)
		AND COALESCE(environnement_id, -1) = COALESCE(?, -1)
		AND COALESCE(techno_id, -1)        = COALESCE(?, -1)
		AND COALESCE(tier_id, -1)          = COALESCE(?, -1)
		AND COALESCE(cluster_id, -1)       = COALESCE(?, -1)`,
		[]any{variableID, annee, scenarioID,
			p.ProjetID, p.EnvironnementID, p.TechnoID, p.TierID, p.ClusterID}
}

func scanValeur(row *sql.Row, contexte string) (ValeurVariable, error) {
	var v ValeurVariable
	err := scanUn(row, contexte,
		&v.ID, &v.VariableID, &v.ScenarioID, &v.Annee, &v.Portee.ProjetID,
		&v.Portee.EnvironnementID, &v.Portee.TechnoID, &v.Portee.TierID, &v.Portee.ClusterID,
		&v.Valeur, &v.Commentaire, &v.ModifieLe, &v.ModifiePar)
	return v, err
}

func scanValeurLignes(l *sql.Rows) (ValeurVariable, error) {
	var v ValeurVariable
	err := l.Scan(
		&v.ID, &v.VariableID, &v.ScenarioID, &v.Annee, &v.Portee.ProjetID,
		&v.Portee.EnvironnementID, &v.Portee.TechnoID, &v.Portee.TierID, &v.Portee.ClusterID,
		&v.Valeur, &v.Commentaire, &v.ModifieLe, &v.ModifiePar)
	if err != nil {
		return ValeurVariable{}, fmt.Errorf("lecture d'une valeur : %w", err)
	}
	return v, nil
}
