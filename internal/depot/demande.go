package depot

import (
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FicheDemande est la vue « à plat » d'un serveur telle que la génération
// des demandes de matériel (backlog v3.2) la consomme : toutes les variables
// du gabarit, déjà résolues en texte. Chaque champ correspond à une variable
// {nom} du gabarit — voir Valeurs. Une donnée absente (serveur hypothétique
// sans IP, sans affectation, sans révision) est une chaîne vide, jamais un
// pointeur : le gabarit rend « vide », et le bloc se recalcule quand la donnée
// arrive.
//
// Le cluster est celui de l'affectation active du seau du serveur : le réel
// pour un serveur réel, son scénario pour un serveur HYPOTHESE. Les totaux
// matériels viennent de la révision rattachée en cours (serveur_revision
// ouverte), quantité × capacité unitaire par code canonique.
type FicheDemande struct {
	ServeurID int64
	// identifiants dans l'outil de demande interne
	NumFiche   string // demande_serveur_ref
	NumDemande string // demande_ref
	// serveur
	NomPhysique string
	Hostname    string
	IP          string
	Vlan        string // code du VLAN
	Zone        string // code de la zone
	Site        string
	Position    string
	Typologie   string
	CodeAppli   string
	Commentaire string
	Statut      string
	DateEntree  string
	Scenario    string // nom du scénario, vide pour le réel
	// périmètre (affectation active)
	Projet        string
	Environnement string
	Techno        string
	Tier          string
	Usage         string
	Cluster       string
	// catalogue (révision rattachée en cours)
	Modele            string // code
	ModeleType        string
	ModeleDescription string
	Revision          string // numéro
	CPU               string
	RAM               string
	HDD               string
	SSD               string
	NIC               string
	GPU               string
}

// Valeurs expose la fiche sous la forme attendue par un gabarit : la clé est
// le nom de la variable tel que le rédacteur l'écrit entre accolades. C'est
// l'unique endroit où les noms du catalogue (backlog v3.2) sont associés aux
// champs ; le catalogue affiché à l'écran (web.VariablesGabarit) doit lister
// exactement ces clés.
func (f FicheDemande) Valeurs() map[string]string {
	return map[string]string{
		"num_fiche":          f.NumFiche,
		"num_demande":        f.NumDemande,
		"nom_physique":       f.NomPhysique,
		"hostname":           f.Hostname,
		"ip":                 f.IP,
		"vlan":               f.Vlan,
		"zone":               f.Zone,
		"site":               f.Site,
		"position":           f.Position,
		"typologie":          f.Typologie,
		"code_appli":         f.CodeAppli,
		"commentaire":        f.Commentaire,
		"projet":             f.Projet,
		"environnement":      f.Environnement,
		"techno":             f.Techno,
		"tier":               f.Tier,
		"usage":              f.Usage,
		"cluster":            f.Cluster,
		"modele":             f.Modele,
		"modele_type":        f.ModeleType,
		"modele_description": f.ModeleDescription,
		"revision":           f.Revision,
		"cpu":                f.CPU,
		"ram":                f.RAM,
		"hdd":                f.HDD,
		"ssd":                f.SSD,
		"nic":                f.NIC,
		"gpu":                f.GPU,
		"statut":             f.Statut,
		"date_entree":        f.DateEntree,
		"scenario":           f.Scenario,
	}
}

// FiltreDemande restreint ListerFichesDemande. Les pointeurs nil ne
// restreignent pas, sauf ScenarioID dont la sémantique est « quel parc » :
// nil, les serveurs réels ; une valeur, les serveurs HYPOTHESE de ce
// scénario (serveur.scenario_id) — une demande porte sur ce qu'on projette
// d'acheter, ou sur ce qui existe, jamais sur la superposition des deux.
//
// Les filtres de périmètre (projet, environnement, cluster) s'appliquent au
// cluster de l'affectation active : un serveur sans affectation n'a pas de
// périmètre et n'est retenu par aucun d'eux.
type FiltreDemande struct {
	ScenarioID      *int64
	ProjetID        *int64
	EnvironnementID *int64
	ClusterID       *int64
	Statut          *string
	DemandeRef      *string // numéro de demande exact
	SansNumero      bool    // demande_ref NULL ou vide
}

// requeteFiches est la lecture commune à ListerFichesDemande et
// LireFicheDemande. L'affectation retenue est l'active (date_fin IS NULL) du
// seau du serveur — COALESCE(…, -1) est la convention de la migration 0002,
// dont l'index partiel garantit qu'il y en a au plus une ; la révision est
// le rattachement ouvert, unique pour la même raison. Les totaux matériels
// sont des sous-requêtes corrélées par code canonique (ComposantsCanoniques) :
// quantité × capacité unitaire, comme le constructeur de vues.
const requeteFiches = `
	SELECT s.id,
	       COALESCE(s.demande_serveur_ref, ''), COALESCE(s.demande_ref, ''),
	       COALESCE(s.physical_name, ''), COALESCE(s.hostname, ''), COALESCE(s.ip, ''),
	       COALESCE(v.code, ''), COALESCE(z.code, ''), COALESCE(z.site, ''),
	       COALESCE(s.position_zone, ''), COALESCE(s.typologie, ''),
	       COALESCE(s.code_appli, ''), COALESCE(s.commentaire, ''),
	       s.statut, COALESCE(s.date_entree, ''), COALESCE(sc.nom, ''),
	       COALESCE(p.code, ''), COALESCE(e.code, ''), COALESCE(t.code, ''),
	       COALESCE(ti.code, ''), COALESCE(u.code, ''), COALESCE(c.nom, ''),
	       COALESCE(m.code, ''), COALESCE(m.type, ''), COALESCE(m.description, ''),
	       rev.numero,
	       (SELECT SUM(quantite * capacite_unitaire) FROM composant WHERE revision_id = rev.id AND code = 'cpu'),
	       (SELECT SUM(quantite * capacite_unitaire) FROM composant WHERE revision_id = rev.id AND code = 'ram'),
	       (SELECT SUM(quantite * capacite_unitaire) FROM composant WHERE revision_id = rev.id AND code = 'hdd'),
	       (SELECT SUM(quantite * capacite_unitaire) FROM composant WHERE revision_id = rev.id AND code = 'ssd'),
	       (SELECT SUM(quantite * capacite_unitaire) FROM composant WHERE revision_id = rev.id AND code = 'nic'),
	       (SELECT SUM(quantite * capacite_unitaire) FROM composant WHERE revision_id = rev.id AND code = 'gpu')
	FROM serveur s
	LEFT JOIN vlan v      ON v.id = s.vlan_id
	LEFT JOIN zone z      ON z.id = s.zone_id
	LEFT JOIN scenario sc ON sc.id = s.scenario_id
	LEFT JOIN affectation a ON a.serveur_id = s.id AND a.date_fin IS NULL
	                       AND COALESCE(a.scenario_id, -1) = COALESCE(s.scenario_id, -1)
	LEFT JOIN cluster c            ON c.id = a.cluster_id
	LEFT JOIN projet p             ON p.id = c.projet_id
	LEFT JOIN environnement e      ON e.id = c.environnement_id
	LEFT JOIN techno t             ON t.id = c.techno_id
	LEFT JOIN tier ti              ON ti.id = c.tier_id
	LEFT JOIN usage_fonctionnel u  ON u.id = c.usage_fonctionnel_id
	LEFT JOIN serveur_revision sr  ON sr.serveur_id = s.id AND sr.date_fin IS NULL
	LEFT JOIN revision rev         ON rev.id = sr.revision_id
	LEFT JOIN modele m             ON m.id = rev.modele_id
	WHERE `

// ListerFichesDemande renvoie les fiches des serveurs retenus par le filtre,
// triées par cluster puis nom physique (les serveurs sans cluster ou sans nom
// en dernier), à identifiant croissant à égalité — un tri stable, pour que
// deux affichages successifs se comparent ligne à ligne.
func (d *Depot) ListerFichesDemande(f FiltreDemande) ([]FicheDemande, error) {
	cond := []string{}
	var args []any
	if f.ScenarioID == nil {
		cond = append(cond, "s.scenario_id IS NULL")
	} else {
		cond = append(cond, "s.scenario_id = ?")
		args = append(args, *f.ScenarioID)
	}
	if f.ProjetID != nil {
		cond = append(cond, "c.projet_id = ?")
		args = append(args, *f.ProjetID)
	}
	if f.EnvironnementID != nil {
		cond = append(cond, "c.environnement_id = ?")
		args = append(args, *f.EnvironnementID)
	}
	if f.ClusterID != nil {
		cond = append(cond, "c.id = ?")
		args = append(args, *f.ClusterID)
	}
	if f.Statut != nil {
		cond = append(cond, "s.statut = ?")
		args = append(args, strings.TrimSpace(*f.Statut))
	}
	if f.DemandeRef != nil {
		cond = append(cond, "s.demande_ref = ?")
		args = append(args, strings.TrimSpace(*f.DemandeRef))
	}
	if f.SansNumero {
		cond = append(cond, "(s.demande_ref IS NULL OR TRIM(s.demande_ref) = '')")
	}

	requete := requeteFiches + strings.Join(cond, " AND ") + `
		ORDER BY c.nom IS NULL, c.nom, s.physical_name IS NULL, s.physical_name, s.id`
	lignes, err := d.base.Query(requete, args...)
	if err != nil {
		return nil, traduire("liste des fiches de demande", err)
	}
	defer lignes.Close()

	var out []FicheDemande
	for lignes.Next() {
		fiche, err := scanFiche(lignes)
		if err != nil {
			return nil, fmt.Errorf("liste des fiches de demande : %w", err)
		}
		out = append(out, fiche)
	}
	return out, lignes.Err()
}

// LireFicheDemande renvoie la fiche d'un serveur, quel que soit son seau :
// c'est la relecture après édition du numéro de fiche, où le serveur est
// déjà identifié.
//
// Erreurs : ErrIntrouvable si le serveur n'existe pas.
func (d *Depot) LireFicheDemande(serveurID int64) (FicheDemande, error) {
	lignes, err := d.base.Query(requeteFiches+"s.id = ?", serveurID)
	if err != nil {
		return FicheDemande{}, traduire(fmt.Sprintf("lecture de la fiche du serveur %d", serveurID), err)
	}
	defer lignes.Close()
	if !lignes.Next() {
		if err := lignes.Err(); err != nil {
			return FicheDemande{}, fmt.Errorf("lecture de la fiche du serveur %d : %w", serveurID, err)
		}
		return FicheDemande{}, fmt.Errorf("lecture de la fiche du serveur %d : %w", serveurID, ErrIntrouvable)
	}
	fiche, err := scanFiche(lignes)
	if err != nil {
		return FicheDemande{}, fmt.Errorf("lecture de la fiche du serveur %d : %w", serveurID, err)
	}
	return fiche, nil
}

func scanFiche(lignes *sql.Rows) (FicheDemande, error) {
	var f FicheDemande
	var numeroRevision *int64
	var cpu, ram, hdd, ssd, nic, gpu *float64
	if err := lignes.Scan(
		&f.ServeurID,
		&f.NumFiche, &f.NumDemande,
		&f.NomPhysique, &f.Hostname, &f.IP,
		&f.Vlan, &f.Zone, &f.Site,
		&f.Position, &f.Typologie,
		&f.CodeAppli, &f.Commentaire,
		&f.Statut, &f.DateEntree, &f.Scenario,
		&f.Projet, &f.Environnement, &f.Techno,
		&f.Tier, &f.Usage, &f.Cluster,
		&f.Modele, &f.ModeleType, &f.ModeleDescription,
		&numeroRevision,
		&cpu, &ram, &hdd, &ssd, &nic, &gpu,
	); err != nil {
		return FicheDemande{}, err
	}
	if numeroRevision != nil {
		f.Revision = strconv.FormatInt(*numeroRevision, 10)
	}
	f.CPU = quantiteTexte(cpu)
	f.RAM = quantiteTexte(ram)
	f.HDD = quantiteTexte(hdd)
	f.SSD = quantiteTexte(ssd)
	f.NIC = quantiteTexte(nic)
	f.GPU = quantiteTexte(gpu)
	return f, nil
}

// quantiteTexte formate un total matériel pour un texte destiné à être
// collé tel quel : arrondi à deux décimales, sans zéros superflus (1024 →
// « 1024 », 24 × 7,68 → « 184.32 »), vide si aucun composant de ce code.
func quantiteTexte(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(math.Round(*v*100)/100, 'f', -1, 64)
}

// DefinirDemandeRef pose le numéro de demande sur un lot de serveurs, dans
// une seule transaction : une campagne affecte son numéro à toute une
// hypothèse ou à une sélection filtrée d'un coup, jamais à moitié. Renvoie le
// nombre de serveurs effectivement touchés (les identifiants inconnus sont
// ignorés, pas une erreur : la liste vient d'un écran qui a pu vieillir).
//
// Le numéro reste porté par le serveur (demande_ref), jamais par le
// scénario. Une référence vide ou blanche est normalisée en NULL : « sans
// numéro » n'a qu'une représentation en base.
func (d *Depot) DefinirDemandeRef(serveurIDs []int64, ref *string) (int, error) {
	if len(serveurIDs) == 0 {
		return 0, nil
	}
	ref = normaliserRef(ref)
	marques := make([]string, len(serveurIDs))
	args := []any{ref}
	for i, id := range serveurIDs {
		marques[i] = "?"
		args = append(args, id)
	}
	var touches int
	err := d.enTx(func(tx *sql.Tx) error {
		// journal : une entrée par serveur réellement touché (un identifiant
		// inconnu est ignoré, comme avant).
		avants := map[int64]map[string]any{}
		for _, id := range serveurIDs {
			avant, err := instantane(tx, "serveur", id)
			if err != nil {
				return err
			}
			if avant != nil {
				avants[id] = avant
			}
		}
		res, err := tx.Exec(
			`UPDATE serveur SET demande_ref = ? WHERE id IN (`+strings.Join(marques, ",")+`)`,
			args...)
		if err != nil {
			return traduire("affectation du numéro de demande", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("affectation du numéro de demande : %w", err)
		}
		touches = int(n)
		for id, avant := range avants {
			if err := d.journalModificationTable(tx, "serveur", "serveur", id, ActionModification, avant); err != nil {
				return err
			}
		}
		return nil
	})
	return touches, err
}

// DefinirDemandeServeurRef pose le numéro de fiche d'un serveur dans la
// demande (demande_serveur_ref). Vide ou blanc devient NULL.
//
// Erreurs : ErrIntrouvable si le serveur n'existe pas.
func (d *Depot) DefinirDemandeServeurRef(serveurID int64, ref *string) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "serveur", serveurID)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE serveur SET demande_serveur_ref = ? WHERE id = ?`, normaliserRef(ref), serveurID)
		if err != nil {
			return traduire(fmt.Sprintf("numéro de fiche du serveur %d", serveurID), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("numéro de fiche du serveur %d", serveurID)); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "serveur", "serveur", serveurID, ActionModification, avant)
	})
}

// normaliserRef recadre une référence et ramène l'absence à nil.
func normaliserRef(ref *string) *string {
	if ref == nil {
		return nil
	}
	v := strings.TrimSpace(*ref)
	if v == "" {
		return nil
	}
	return &v
}
