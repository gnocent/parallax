package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Composant est un élément matériel d'une révision : sa nature, sa quantité et
// sa capacité unitaire. Il remplace les colonnes CPU / RAM / DISK précalculées
// de l'Excel — la capacité brute reste en base, les coefficients (formatage,
// remplissage, compression) vivent dans les variables des formules.
//
// Le champ Code est la variable exposée aux formules (« ssd », « cpu »,
// « ram ») ; il est unique au sein d'une révision. Les codes canoniques —
// ceux que l'import et le constructeur de vues connaissent — sont listés
// dans ComposantsCanoniques ; un code libre reste possible pour un composant
// qui n'entre dans aucune case (nature AUTRE) : il est alors visible des
// formules mais pas des colonnes de vues.
type Composant struct {
	ID               int64
	RevisionID       int64
	Nature           string // CPU|RAM|DISQUE_DATA|GPU|NIC|AUTRE, contrôlé par la base
	Code             string
	Quantite         float64
	CapaciteUnitaire float64
	Unite            string // TO|GO|CORE|GBPS|POINT|TFLOPS|TOS|UNITE, contrôlé par la base
	Commentaire      *string
}

// ComposantCanonique décrit un code de composant connu de toute
// l'application : l'import des modèles le produit depuis ses colonnes CSV,
// le constructeur de vues l'agrège, les formules le lisent sous
// « <code>_total » et « <code>_machine ».
//
// Deux formes, selon Scalaire :
//   - scalaire (cpu, specrate, phoronix, ram) : quantité 1, la capacité
//     unitaire porte la valeur totale du serveur — un nombre de cœurs, un
//     score, une quantité de Go. On ne décompose pas en sockets ni en
//     barrettes : c'est le total qui sert au calcul ;
//   - dénombré (hdd, ssd, nic, gpu et ses attributs) : quantité × capacité
//     unitaire. Les trois attributs GPU (gpu_ram, gpu_fp8, gpu_bp) partagent
//     la quantité du composant gpu — seule redondance admise, parce que
//     chaque attribut doit rester une variable distincte des formules.
type ComposantCanonique struct {
	Code      string
	Nature    string
	Unite     string
	Scalaire  bool
	Libelle   string // ce que « <code>_total » mesure (quantité × capacité unitaire)
	LibelleNb string // ce que la quantité seule mesure ; vide pour un scalaire ou un simple dénombrement
}

// ComposantsCanoniques est le vocabulaire matériel de l'application, dans
// l'ordre d'affichage. Voir docs/modele-donnees.md §4.2 et
// docs/import-format.md.
var ComposantsCanoniques = []ComposantCanonique{
	{Code: "cpu", Nature: "CPU", Unite: "CORE", Scalaire: true, Libelle: "Cœurs CPU"},
	{Code: "specrate", Nature: "CPU", Unite: "POINT", Scalaire: true, Libelle: "SPECrate base"},
	{Code: "phoronix", Nature: "CPU", Unite: "POINT", Scalaire: true, Libelle: "Phoronix"},
	{Code: "ram", Nature: "RAM", Unite: "GO", Scalaire: true, Libelle: "RAM (Go)"},
	{Code: "hdd", Nature: "DISQUE_DATA", Unite: "TO", Libelle: "HDD (To)", LibelleNb: "HDD (nb)"},
	{Code: "ssd", Nature: "DISQUE_DATA", Unite: "TO", Libelle: "SSD (To)", LibelleNb: "SSD (nb)"},
	{Code: "nic", Nature: "NIC", Unite: "GBPS", Libelle: "Réseau (Gbps)", LibelleNb: "NIC (nb)"},
	{Code: "gpu", Nature: "GPU", Unite: "UNITE", Libelle: "GPU (nb)"},
	{Code: "gpu_ram", Nature: "GPU", Unite: "GO", Libelle: "RAM GPU (Go)"},
	{Code: "gpu_fp8", Nature: "GPU", Unite: "TFLOPS", Libelle: "GPU FP8 (TFLOPS)"},
	{Code: "gpu_bp", Nature: "GPU", Unite: "TOS", Libelle: "Bande passante GPU (To/s)"},
}

// valider vérifie les règles indépendantes de la base. Nature et unité sont
// laissées aux CHECK SQLite (traduits en ErrValidation).
func (c *Composant) valider(contexte string) error {
	c.Code = strings.TrimSpace(c.Code)
	if c.Code == "" {
		return fmt.Errorf("%s : %w : code vide", contexte, ErrValidation)
	}
	if c.Quantite <= 0 {
		return fmt.Errorf("%s : %w : quantité « %g » non positive", contexte, ErrValidation, c.Quantite)
	}
	if c.CapaciteUnitaire <= 0 {
		return fmt.Errorf("%s : %w : capacité unitaire « %g » non positive", contexte, ErrValidation, c.CapaciteUnitaire)
	}
	return nil
}

// AjouterComposant ajoute un composant à une révision et renvoie la ligne
// complète, identifiant attribué.
//
// Invariant 1 : si la révision est déjà référencée par un serveur_revision et
// que correction vaut false, l'ajout est refusé avec ErrImmuable — il faut
// créer une nouvelle révision. correction == true force l'ajout.
//
// Erreurs : ErrValidation (code vide, quantité ou capacité <= 0, nature ou
// unité hors énumération) ; ErrReference si la révision n'existe pas ;
// ErrConflit si le code est déjà pris dans la révision ; ErrImmuable comme
// décrit ci-dessus.
func (d *Depot) AjouterComposant(c Composant, correction bool) (Composant, error) {
	if err := c.valider("ajout d'un composant"); err != nil {
		return Composant{}, err
	}

	err := d.enTx(func(tx *sql.Tx) error {
		if err := garantirRevisionModifiable(tx, c.RevisionID, correction); err != nil {
			return err
		}
		res, err := tx.Exec(
			`INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite, commentaire)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			c.RevisionID, c.Nature, c.Code, c.Quantite, c.CapaciteUnitaire, c.Unite, c.Commentaire)
		if err != nil {
			return traduire(fmt.Sprintf("ajout d'un composant à la révision %d", c.RevisionID), err)
		}
		c.ID, _ = res.LastInsertId()
		return d.journaliser(tx, "composant", c.ID, actionCatalogue(correction, ActionCreation), nil, c)
	})
	if err != nil {
		return Composant{}, err
	}
	return c, nil
}

// ModifierComposant met à jour la nature, le code, la quantité, la capacité
// unitaire, l'unité et le commentaire d'un composant. Le rattachement à la
// révision n'est pas modifiable.
//
// Invariant 1 : mêmes règles qu'AjouterComposant, évaluées sur la révision
// portant actuellement le composant.
//
// Erreurs : ErrValidation ; ErrIntrouvable si le composant n'existe pas ;
// ErrConflit sur le code ; ErrImmuable.
func (d *Depot) ModifierComposant(c Composant, correction bool) error {
	if err := c.valider(fmt.Sprintf("modification du composant %d", c.ID)); err != nil {
		return err
	}

	return d.enTx(func(tx *sql.Tx) error {
		var revisionID int64
		if err := scanUn(
			tx.QueryRow(`SELECT revision_id FROM composant WHERE id = ?`, c.ID),
			fmt.Sprintf("modification du composant %d", c.ID),
			&revisionID); err != nil {
			return err
		}
		if err := garantirRevisionModifiable(tx, revisionID, correction); err != nil {
			return err
		}
		avant, err := instantane(tx, "composant", c.ID)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE composant SET nature = ?, code = ?, quantite = ?,
				capacite_unitaire = ?, unite = ?, commentaire = ?
			 WHERE id = ?`,
			c.Nature, c.Code, c.Quantite, c.CapaciteUnitaire, c.Unite, c.Commentaire, c.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification du composant %d", c.ID), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("modification du composant %d", c.ID)); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "composant", "composant", c.ID, actionCatalogue(correction, ActionModification), avant)
	})
}

// SupprimerComposant retire un composant d'une révision.
//
// Invariant 1 : refusé avec ErrImmuable si la révision est référencée et
// correction vaut false.
//
// Erreurs : ErrIntrouvable si le composant n'existe pas ; ErrImmuable.
func (d *Depot) SupprimerComposant(id int64, correction bool) error {
	return d.enTx(func(tx *sql.Tx) error {
		var revisionID int64
		if err := scanUn(
			tx.QueryRow(`SELECT revision_id FROM composant WHERE id = ?`, id),
			fmt.Sprintf("suppression du composant %d", id),
			&revisionID); err != nil {
			return err
		}
		if err := garantirRevisionModifiable(tx, revisionID, correction); err != nil {
			return err
		}
		avant, err := instantane(tx, "composant", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM composant WHERE id = ?`, id)
		if err != nil {
			return traduire(fmt.Sprintf("suppression du composant %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("suppression du composant %d", id)); err != nil {
			return err
		}
		return d.journalSuppressionTable(tx, "composant", id, avant)
	})
}

// actionCatalogue : une écriture sur une révision référencée, forcée par
// « corriger », se journalise en CORRECTION plutôt que sous l'action
// normale — c'est le geste que le journal doit rendre visible en premier.
func actionCatalogue(correction bool, normale string) string {
	if correction {
		return ActionCorrection
	}
	return normale
}

// ListerComposants renvoie les composants d'une révision, triés par code.
func (d *Depot) ListerComposants(revisionID int64) ([]Composant, error) {
	lignes, err := d.base.Query(
		`SELECT id, revision_id, nature, code, quantite, capacite_unitaire, unite, commentaire
		 FROM composant WHERE revision_id = ? ORDER BY code`, revisionID)
	if err != nil {
		return nil, traduire(fmt.Sprintf("liste des composants de la révision %d", revisionID), err)
	}
	defer lignes.Close()

	var out []Composant
	for lignes.Next() {
		var c Composant
		if err := lignes.Scan(&c.ID, &c.RevisionID, &c.Nature, &c.Code,
			&c.Quantite, &c.CapaciteUnitaire, &c.Unite, &c.Commentaire); err != nil {
			return nil, fmt.Errorf("liste des composants de la révision %d : %w", revisionID, err)
		}
		out = append(out, c)
	}
	return out, lignes.Err()
}
