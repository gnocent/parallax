package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Cluster est le périmètre de calcul : l'intersection concrète des dimensions
// projet × environnement × techno × tier × usage, plus un nom libre. C'est à lui
// qu'on affecte les serveurs et que les règles de capacité s'appliquent.
//
// Les trois premières dimensions sont obligatoires ; tier et usage sont
// facultatifs (Kafka et Nomad n'ont pas de tier), d'où des pointeurs. Le cluster
// se désactive (actif = 0) plutôt que de se supprimer : les affectations,
// scénarios et règles qui le référencent restent lisibles.
type Cluster struct {
	ID                 int64
	Nom                string
	ProjetID           int64
	EnvironnementID    int64
	TechnoID           int64
	TierID             *int64
	UsageFonctionnelID *int64
	Commentaire        *string
	Actif              bool
}

// FiltreCluster restreint ListerClusters. Tous les champs *int64 sont
// facultatifs : nil ne restreint pas la dimension correspondante. Par défaut
// seuls les clusters actifs sont renvoyés ; InclureInactifs lève cette borne.
type FiltreCluster struct {
	ProjetID        *int64
	EnvironnementID *int64
	TechnoID        *int64
	TierID          *int64
	UsageID         *int64
	InclureInactifs bool
}

const colonnesCluster = `id, nom, projet_id, environnement_id, techno_id,
	tier_id, usage_fonctionnel_id, commentaire, actif`

// CreerCluster insère un cluster et renvoie la ligne complète, identifiant
// attribué. Le cluster est actif par défaut.
//
// Erreurs : ErrValidation si le nom est vide ou si l'une des trois dimensions
// obligatoires (projet, environnement, techno) n'est pas strictement positive ;
// ErrConflit si le triplet (projet, environnement, nom) est déjà pris ;
// ErrReference si une dimension désigne un référentiel inexistant.
func (d *Depot) CreerCluster(c Cluster) (Cluster, error) {
	c.Nom = strings.TrimSpace(c.Nom)
	if err := validerCluster(c); err != nil {
		return Cluster{}, err
	}

	return creerJournalise(d, "cluster", func(tx *sql.Tx) (Cluster, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO cluster
			 (nom, projet_id, environnement_id, techno_id, tier_id,
			  usage_fonctionnel_id, commentaire)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			c.Nom, c.ProjetID, c.EnvironnementID, c.TechnoID,
			c.TierID, c.UsageFonctionnelID, c.Commentaire)
		if err != nil {
			return Cluster{}, 0, traduire("création du cluster "+c.Nom, err)
		}
		c.ID, _ = res.LastInsertId()
		c.Actif = true
		return c, c.ID, nil
	})
}

// LireCluster renvoie un cluster par identifiant, ou ErrIntrouvable.
func (d *Depot) LireCluster(id int64) (Cluster, error) {
	return lireCluster(d.base, id)
}

// ListerClusters renvoie les clusters retenus par le filtre, triés par nom.
// Le WHERE est construit dynamiquement à partir des champs non nil du filtre.
func (d *Depot) ListerClusters(f FiltreCluster) ([]Cluster, error) {
	var cond []string
	var args []any
	if !f.InclureInactifs {
		cond = append(cond, "actif = 1")
	}
	for _, p := range []struct {
		col string
		val *int64
	}{
		{"projet_id", f.ProjetID},
		{"environnement_id", f.EnvironnementID},
		{"techno_id", f.TechnoID},
		{"tier_id", f.TierID},
		{"usage_fonctionnel_id", f.UsageID},
	} {
		if p.val != nil {
			cond = append(cond, p.col+" = ?")
			args = append(args, *p.val)
		}
	}

	requete := `SELECT ` + colonnesCluster + ` FROM cluster`
	if len(cond) > 0 {
		requete += ` WHERE ` + strings.Join(cond, " AND ")
	}
	requete += ` ORDER BY nom`

	lignes, err := d.base.Query(requete, args...)
	if err != nil {
		return nil, traduire("liste des clusters", err)
	}
	defer lignes.Close()

	var out []Cluster
	for lignes.Next() {
		c, err := scanCluster(lignes)
		if err != nil {
			return nil, fmt.Errorf("liste des clusters : %w", err)
		}
		out = append(out, c)
	}
	return out, lignes.Err()
}

// ModifierCluster met à jour le nom, les six dimensions et le commentaire.
// L'activité se pilote par ArchiverCluster / ReactiverCluster.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation,
// ErrConflit et ErrReference comme pour CreerCluster.
func (d *Depot) ModifierCluster(c Cluster) error {
	c.Nom = strings.TrimSpace(c.Nom)
	if err := validerCluster(c); err != nil {
		return err
	}

	return modifierJournalise(d, "cluster", c.ID, ActionModification, lireCluster, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE cluster SET
			   nom = ?, projet_id = ?, environnement_id = ?, techno_id = ?,
			   tier_id = ?, usage_fonctionnel_id = ?, commentaire = ?
			 WHERE id = ?`,
			c.Nom, c.ProjetID, c.EnvironnementID, c.TechnoID,
			c.TierID, c.UsageFonctionnelID, c.Commentaire, c.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification du cluster %d", c.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification du cluster %d", c.ID))
	})
}

// ArchiverCluster désactive un cluster sans le supprimer : il disparaît des
// listes courantes mais reste lisible avec InclureInactifs, et son historique
// d'affectations est préservé.
func (d *Depot) ArchiverCluster(id int64) error {
	return modifierJournalise(d, "cluster", id, ActionModification, lireCluster, func(tx *sql.Tx) error {
		return basculerActif(tx, "cluster", id, false)
	})
}

// ReactiverCluster réactive un cluster archivé.
func (d *Depot) ReactiverCluster(id int64) error {
	return modifierJournalise(d, "cluster", id, ActionModification, lireCluster, func(tx *sql.Tx) error {
		return basculerActif(tx, "cluster", id, true)
	})
}

// ---------------------------------------------------------------- helpers

func validerCluster(c Cluster) error {
	if c.Nom == "" {
		return fmt.Errorf("cluster : %w : nom vide", ErrValidation)
	}
	if c.ProjetID <= 0 || c.EnvironnementID <= 0 || c.TechnoID <= 0 {
		return fmt.Errorf(
			"cluster %q : %w : projet, environnement et techno sont obligatoires",
			c.Nom, ErrValidation)
	}
	return nil
}

// scanCluster lit une ligne de la projection colonnesCluster.
func scanCluster(s interface{ Scan(...any) error }) (Cluster, error) {
	var c Cluster
	err := s.Scan(&c.ID, &c.Nom, &c.ProjetID, &c.EnvironnementID, &c.TechnoID,
		&c.TierID, &c.UsageFonctionnelID, &c.Commentaire, &c.Actif)
	return c, err
}

func lireCluster(c conn, id int64) (Cluster, error) {
	var cl Cluster
	err := scanUn(
		c.QueryRow(`SELECT `+colonnesCluster+` FROM cluster WHERE id = ?`, id),
		fmt.Sprintf("lecture du cluster %d", id),
		&cl.ID, &cl.Nom, &cl.ProjetID, &cl.EnvironnementID, &cl.TechnoID,
		&cl.TierID, &cl.UsageFonctionnelID, &cl.Commentaire, &cl.Actif)
	return cl, err
}
