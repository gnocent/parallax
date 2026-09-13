package depot

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Vlan est une entrée du catalogue réseau (backlog v3.1) : un code et des
// critères d'application — projet, environnement, zone, cluster — dont
// chacun à NULL vaut « tous ». Un serveur se voit proposer les VLAN dont
// chaque critère renseigné correspond à son affectation et à sa zone.
//
// DerniereIP est le curseur d'attribution : la dernière adresse proposée
// dans ce VLAN. La proposition suivante repart après lui, en rebouclant au
// début de la première plage, pour qu'une adresse libérée ne soit reprise
// qu'au tour suivant (le temps qu'un décommissionnement se termine).
//
// Plages est renseigné par les lectures (LireVlan, ListerVlans,
// VlansApplicables) et ignoré par les écritures du VLAN lui-même : les
// plages ont leur propre CRUD.
type Vlan struct {
	ID              int64
	Code            string
	ProjetID        *int64
	EnvironnementID *int64
	ZoneID          *int64
	ClusterID       *int64
	DerniereIP      *string
	Commentaire     *string
	Plages          []PlageIP
}

// PlageIP est un intervalle fermé d'adresses d'un VLAN. Passerelle et
// adresses réservées sont simplement laissées hors intervalle.
type PlageIP struct {
	ID      int64
	VlanID  int64
	IPDebut string
	IPFin   string
}

// Intervalle renvoie la plage sous forme numérique. Une plage lue en base a
// toujours été validée à l'écriture ; l'erreur ne peut venir que d'une
// modification directe de la base.
func (p PlageIP) Intervalle() (IntervalleIPv4, error) {
	return ParserIntervalleIPv4(p.IPDebut, p.IPFin)
}

// NbCriteres compte les critères renseignés : c'est la spécificité du VLAN,
// qui ordonne les candidats (VlansApplicables).
func (v Vlan) NbCriteres() int {
	n := 0
	for _, c := range []*int64{v.ProjetID, v.EnvironnementID, v.ZoneID, v.ClusterID} {
		if c != nil {
			n++
		}
	}
	return n
}

const colonnesVlan = `id, code, projet_id, environnement_id, zone_id, cluster_id, derniere_ip, commentaire`

func scanVlan(s interface{ Scan(...any) error }) (Vlan, error) {
	var v Vlan
	err := s.Scan(&v.ID, &v.Code, &v.ProjetID, &v.EnvironnementID, &v.ZoneID,
		&v.ClusterID, &v.DerniereIP, &v.Commentaire)
	return v, err
}

// ---------------------------------------------------------------- VLAN

// CreerVlan insère un VLAN et renvoie la ligne complète, identifiant attribué.
// Un commentaire vide est normalisé en nil ; un curseur renseigné doit être
// une IPv4 valide. Le champ Plages est ignoré.
//
// Erreurs : ErrValidation si le code est vide ou le curseur invalide ;
// ErrConflit si le code est déjà pris ; ErrReference si un critère pointe
// une entité inexistante.
func (d *Depot) CreerVlan(v Vlan) (Vlan, error) {
	if err := normaliserVlan(&v); err != nil {
		return Vlan{}, fmt.Errorf("création d'un VLAN : %w", err)
	}
	return creerJournalise(d, "vlan", func(tx *sql.Tx) (Vlan, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO vlan (code, projet_id, environnement_id, zone_id, cluster_id, derniere_ip, commentaire)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			v.Code, v.ProjetID, v.EnvironnementID, v.ZoneID, v.ClusterID, v.DerniereIP, v.Commentaire)
		if err != nil {
			return Vlan{}, 0, traduire("création du VLAN "+v.Code, err)
		}
		v.ID, _ = res.LastInsertId()
		v.Plages = nil
		return v, v.ID, nil
	})
}

// LireVlan renvoie un VLAN et ses plages, ou ErrIntrouvable.
func (d *Depot) LireVlan(id int64) (Vlan, error) {
	return lireVlan(d.base, id)
}

// LireVlanParCode renvoie un VLAN et ses plages par son code, ou ErrIntrouvable.
func (d *Depot) LireVlanParCode(code string) (Vlan, error) {
	code = strings.TrimSpace(code)
	v, err := scanVlan(d.base.QueryRow(`SELECT `+colonnesVlan+` FROM vlan WHERE code = ?`, code))
	if err == sql.ErrNoRows {
		return Vlan{}, fmt.Errorf("lecture du VLAN %s : %w", code, ErrIntrouvable)
	}
	if err != nil {
		return Vlan{}, fmt.Errorf("lecture du VLAN %s : %w", code, err)
	}
	if err := chargerPlages(d.base, []*Vlan{&v}); err != nil {
		return Vlan{}, err
	}
	return v, nil
}

// ListerVlans renvoie tous les VLAN triés par code, chacun avec ses plages
// triées par adresse de début.
func (d *Depot) ListerVlans() ([]Vlan, error) {
	return listerVlans(d.base, "", nil)
}

// ModifierVlan met à jour le code, les critères, le curseur et le
// commentaire. Le curseur est modifiable pour permettre de le remettre à
// zéro (nil) ou de le repositionner ; les plages ne sont pas touchées.
//
// Erreurs : ErrIntrouvable ; ErrValidation, ErrConflit et ErrReference comme
// pour CreerVlan.
func (d *Depot) ModifierVlan(v Vlan) error {
	if err := normaliserVlan(&v); err != nil {
		return fmt.Errorf("modification du VLAN %d : %w", v.ID, err)
	}
	return modifierJournalise(d, "vlan", v.ID, ActionModification, lireVlan, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE vlan SET code = ?, projet_id = ?, environnement_id = ?, zone_id = ?,
			        cluster_id = ?, derniere_ip = ?, commentaire = ?
			 WHERE id = ?`,
			v.Code, v.ProjetID, v.EnvironnementID, v.ZoneID, v.ClusterID, v.DerniereIP, v.Commentaire, v.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification du VLAN %d", v.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification du VLAN %d", v.ID))
	})
}

// SupprimerVlan retire un VLAN et ses plages (cascade). Le catalogue est une
// donnée de référence corrigeable, pas un historique : c'est l'exception
// assumée à « rien n'est jamais détruit ». Un VLAN encore porté par un
// serveur est refusé (ErrReference) : le retirer d'abord des serveurs.
func (d *Depot) SupprimerVlan(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "vlan", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM vlan WHERE id = ?`, id)
		if err != nil {
			return traduire(fmt.Sprintf("suppression du VLAN %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("suppression du VLAN %d", id)); err != nil {
			return err
		}
		return d.journalSuppressionTable(tx, "vlan", id, avant)
	})
}

// ---------------------------------------------------------------- plages

// CreerPlage ajoute un intervalle à un VLAN. Les bornes sont normalisées en
// forme canonique.
//
// Erreurs : ErrValidation si une borne est invalide ou début > fin ;
// ErrChevauchement si l'intervalle recouvre une plage existante du même
// VLAN ; ErrReference si le VLAN n'existe pas.
func (d *Depot) CreerPlage(p PlageIP) (PlageIP, error) {
	err := d.enTx(func(tx *sql.Tx) error {
		it, err := validerPlage(tx, p, 0)
		if err != nil {
			return err
		}
		p.IPDebut, p.IPFin = FormaterIPv4(it.Debut), FormaterIPv4(it.Fin)
		res, err := tx.Exec(`INSERT INTO plage_ip (vlan_id, ip_debut, ip_fin) VALUES (?, ?, ?)`,
			p.VlanID, p.IPDebut, p.IPFin)
		if err != nil {
			return traduire(fmt.Sprintf("création d'une plage du VLAN %d", p.VlanID), err)
		}
		p.ID, _ = res.LastInsertId()
		return d.journaliser(tx, "plage_ip", p.ID, ActionCreation, nil, p)
	})
	if err != nil {
		return PlageIP{}, err
	}
	return p, nil
}

// ModifierPlage remplace les bornes d'une plage, mêmes règles que CreerPlage.
// Le VLAN d'une plage ne change pas : supprimer et recréer.
func (d *Depot) ModifierPlage(p PlageIP) error {
	return d.enTx(func(tx *sql.Tx) error {
		actuelle, err := lirePlage(tx, p.ID)
		if err != nil {
			return err
		}
		p.VlanID = actuelle.VlanID
		it, err := validerPlage(tx, p, p.ID)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE plage_ip SET ip_debut = ?, ip_fin = ? WHERE id = ?`,
			FormaterIPv4(it.Debut), FormaterIPv4(it.Fin), p.ID); err != nil {
			return traduire(fmt.Sprintf("modification de la plage %d", p.ID), err)
		}
		apres, err := lirePlage(tx, p.ID)
		if err != nil {
			return err
		}
		return d.journaliser(tx, "plage_ip", p.ID, ActionModification, actuelle, apres)
	})
}

// LirePlage renvoie une plage par identifiant, ou ErrIntrouvable.
func (d *Depot) LirePlage(id int64) (PlageIP, error) {
	return lirePlage(d.base, id)
}

// SupprimerPlage retire une plage. Les adresses déjà attribuées restent sur
// les serveurs : elles apparaîtront comme hors plage dans les anomalies.
func (d *Depot) SupprimerPlage(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "plage_ip", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM plage_ip WHERE id = ?`, id)
		if err != nil {
			return traduire(fmt.Sprintf("suppression de la plage %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("suppression de la plage %d", id)); err != nil {
			return err
		}
		return d.journalSuppressionTable(tx, "plage_ip", id, avant)
	})
}

// ---------------------------------------------------------------- applicabilité

// VlansApplicables renvoie les VLAN dont chaque critère renseigné correspond
// au contexte d'un serveur : le projet et l'environnement de son cluster, sa
// zone (nil = serveur sans zone : un VLAN qui exige une zone ne s'applique
// pas) et son cluster. Un critère NULL sur le VLAN vaut « tous ».
//
// Triés du plus spécifique (le plus de critères renseignés) au moins
// spécifique, puis par code — l'appelant choisit ou fait choisir.
func (d *Depot) VlansApplicables(projetID, environnementID int64, zoneID *int64, clusterID int64) ([]Vlan, error) {
	return vlansApplicables(d.base, projetID, environnementID, zoneID, clusterID)
}

func vlansApplicables(c conn, projetID, environnementID int64, zoneID *int64, clusterID int64) ([]Vlan, error) {
	zone := int64(-1) // ne correspond à aucune zone : seuls les VLAN sans critère de zone passent
	if zoneID != nil {
		zone = *zoneID
	}
	return listerVlans(c,
		`WHERE (projet_id IS NULL OR projet_id = ?)
		   AND (environnement_id IS NULL OR environnement_id = ?)
		   AND (zone_id IS NULL OR zone_id = ?)
		   AND (cluster_id IS NULL OR cluster_id = ?)`,
		[]any{projetID, environnementID, zone, clusterID})
}

// Applicable indique si ce VLAN s'applique au contexte donné — la même
// règle que VlansApplicables, évaluée en mémoire pour les anomalies.
func (v Vlan) Applicable(projetID, environnementID int64, zoneID *int64, clusterID int64) bool {
	if v.ProjetID != nil && *v.ProjetID != projetID {
		return false
	}
	if v.EnvironnementID != nil && *v.EnvironnementID != environnementID {
		return false
	}
	if v.ZoneID != nil && (zoneID == nil || *v.ZoneID != *zoneID) {
		return false
	}
	if v.ClusterID != nil && *v.ClusterID != clusterID {
		return false
	}
	return true
}

// ---------------------------------------------------------------- helpers

// normaliserVlan recadre le code et le commentaire, met le curseur en forme
// canonique, et refuse un code vide ou un curseur invalide.
func normaliserVlan(v *Vlan) error {
	v.Code = strings.TrimSpace(v.Code)
	if v.Code == "" {
		return fmt.Errorf("%w : code vide", ErrValidation)
	}
	v.Commentaire = normaliserSite(v.Commentaire)
	if v.DerniereIP != nil {
		if strings.TrimSpace(*v.DerniereIP) == "" {
			v.DerniereIP = nil
		} else {
			ip, err := ParserIPv4(*v.DerniereIP)
			if err != nil {
				return fmt.Errorf("curseur : %w", err)
			}
			canonique := FormaterIPv4(ip)
			v.DerniereIP = &canonique
		}
	}
	return nil
}

func lireVlan(c conn, id int64) (Vlan, error) {
	v, err := scanVlan(c.QueryRow(`SELECT `+colonnesVlan+` FROM vlan WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return Vlan{}, fmt.Errorf("lecture du VLAN %d : %w", id, ErrIntrouvable)
	}
	if err != nil {
		return Vlan{}, fmt.Errorf("lecture du VLAN %d : %w", id, err)
	}
	if err := chargerPlages(c, []*Vlan{&v}); err != nil {
		return Vlan{}, err
	}
	return v, nil
}

// listerVlans lit les VLAN satisfaisant la clause (WHERE … ou vide), du
// plus spécifique au moins spécifique puis par code, et charge leurs plages.
func listerVlans(c conn, clause string, args []any) ([]Vlan, error) {
	lignes, err := c.Query(`SELECT `+colonnesVlan+` FROM vlan `+clause+`
		ORDER BY (projet_id IS NOT NULL) + (environnement_id IS NOT NULL)
		       + (zone_id IS NOT NULL) + (cluster_id IS NOT NULL) DESC, code`, args...)
	if err != nil {
		return nil, traduire("liste des VLAN", err)
	}
	defer lignes.Close()

	var out []Vlan
	for lignes.Next() {
		v, err := scanVlan(lignes)
		if err != nil {
			return nil, fmt.Errorf("liste des VLAN : %w", err)
		}
		out = append(out, v)
	}
	if err := lignes.Err(); err != nil {
		return nil, fmt.Errorf("liste des VLAN : %w", err)
	}
	ptrs := make([]*Vlan, len(out))
	for i := range out {
		ptrs[i] = &out[i]
	}
	if err := chargerPlages(c, ptrs); err != nil {
		return nil, err
	}
	return out, nil
}

// chargerPlages renseigne Plages de chaque VLAN, triées numériquement par
// adresse de début. Une seule requête, quel que soit le nombre de VLAN.
func chargerPlages(c conn, vlans []*Vlan) error {
	if len(vlans) == 0 {
		return nil
	}
	parID := make(map[int64]*Vlan, len(vlans))
	marques := make([]string, 0, len(vlans))
	args := make([]any, 0, len(vlans))
	for _, v := range vlans {
		v.Plages = nil
		parID[v.ID] = v
		marques = append(marques, "?")
		args = append(args, v.ID)
	}
	lignes, err := c.Query(`SELECT id, vlan_id, ip_debut, ip_fin FROM plage_ip
		WHERE vlan_id IN (`+strings.Join(marques, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("plages des VLAN : %w", err)
	}
	defer lignes.Close()
	for lignes.Next() {
		var p PlageIP
		if err := lignes.Scan(&p.ID, &p.VlanID, &p.IPDebut, &p.IPFin); err != nil {
			return fmt.Errorf("plages des VLAN : %w", err)
		}
		if v := parID[p.VlanID]; v != nil {
			v.Plages = append(v.Plages, p)
		}
	}
	if err := lignes.Err(); err != nil {
		return fmt.Errorf("plages des VLAN : %w", err)
	}
	for _, v := range vlans {
		trierPlages(v.Plages)
	}
	return nil
}

// trierPlages ordonne par adresse de début numérique ; une borne invalide
// (base modifiée à la main) se retrouve en tête, où elle se voit.
func trierPlages(plages []PlageIP) {
	sort.SliceStable(plages, func(i, j int) bool {
		a, errA := ParserIPv4(plages[i].IPDebut)
		b, errB := ParserIPv4(plages[j].IPDebut)
		if errA != nil || errB != nil {
			return errA != nil && errB == nil
		}
		return a < b
	})
}

func lirePlage(c conn, id int64) (PlageIP, error) {
	var p PlageIP
	err := scanUn(
		c.QueryRow(`SELECT id, vlan_id, ip_debut, ip_fin FROM plage_ip WHERE id = ?`, id),
		fmt.Sprintf("lecture de la plage %d", id),
		&p.ID, &p.VlanID, &p.IPDebut, &p.IPFin)
	return p, err
}

// validerPlage vérifie les bornes, l'existence du VLAN et l'absence de
// chevauchement avec les autres plages du VLAN (exclureID : la plage en
// cours de modification). Renvoie l'intervalle numérique.
func validerPlage(c conn, p PlageIP, exclureID int64) (IntervalleIPv4, error) {
	it, err := ParserIntervalleIPv4(p.IPDebut, p.IPFin)
	if err != nil {
		return IntervalleIPv4{}, fmt.Errorf("plage du VLAN %d : %w", p.VlanID, err)
	}
	v, err := lireVlan(c, p.VlanID)
	if err != nil {
		if errors.Is(err, ErrIntrouvable) {
			return IntervalleIPv4{}, fmt.Errorf("plage : %w : VLAN %d inexistant", ErrReference, p.VlanID)
		}
		return IntervalleIPv4{}, err
	}
	for _, autre := range v.Plages {
		if autre.ID == exclureID {
			continue
		}
		ia, err := autre.Intervalle()
		if err != nil {
			continue
		}
		if it.Chevauche(ia) {
			return IntervalleIPv4{}, fmt.Errorf("plage %s–%s du VLAN %s : %w avec la plage %s–%s",
				FormaterIPv4(it.Debut), FormaterIPv4(it.Fin), v.Code, ErrChevauchement,
				autre.IPDebut, autre.IPFin)
		}
	}
	return it, nil
}
