package web

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"parallax/internal/depot"
	"parallax/internal/importation"
)

// routesImportVlans enregistre l'import du catalogue réseau (backlog v3.1) :
// VLAN et plages dans un seul fichier, une ligne par plage. Le VLAN est
// créé une fois pour son code, avec les critères de sa première ligne ; les
// lignes suivantes du même code doivent porter les mêmes critères (une
// incohérence est une erreur, pas un arbitrage silencieux). Une ligne sans
// bornes crée le VLAN seul.
//
// Même discipline que les autres imports : analyse sans écriture,
// confirmation dans une transaction unique, jamais d'import partiel. Un
// code déjà en base est une erreur de ligne.
func (s *serveur) routesImportVlans() {
	s.mux.HandleFunc("GET /import/vlans", s.lecteur(s.importVlansPage))
	s.mux.HandleFunc("POST /import/vlans/analyser", s.editeur(s.importVlansAnalyser))
	s.mux.HandleFunc("POST /import/vlans/confirmer", s.editeur(s.importVlansConfirmer))
}

func (s *serveur) importVlansPage(w http.ResponseWriter, r *http.Request) {
	s.rendrePage(w, r, s.titre(r, "titre.import_vlans"), "import_vlans_page", nil)
}

// ---------------------------------------------------------------- format

var entetesObligatoiresImportVlans = []string{"code"}

// vlanImport est un VLAN validé avec ses plages, prêt à écrire.
type vlanImport struct {
	NumeroLigne int // première ligne du code
	Vlan        depot.Vlan
	Plages      []depot.PlageIP
	intervalles []depot.IntervalleIPv4
	criteres    string // critères textuels de la première ligne, pour la cohérence
}

// criteresTexte est la signature des critères d'une ligne, comparée entre
// les lignes d'un même code.
func criteresTexte(ligne map[string]string) string {
	parts := make([]string, 0, 4)
	for _, col := range []string{"projet_code", "environnement_code", "zone_code", "cluster_nom"} {
		parts = append(parts, strings.TrimSpace(ligne[col]))
	}
	return strings.Join(parts, "\x1f")
}

// ---------------------------------------------------------------- validation

// validerLignesVlans remonte toutes les anomalies du fichier ensemble. Le
// second retour (VLAN dans l'ordre de première apparition) n'a de sens que
// sans erreur. Le troisième est une panne, jamais une anomalie de saisie.
func validerLignesVlans(d *depot.Depot, entetes []string, lignes []map[string]string, numeros []int) (importation.Rapport, []vlanImport, error) {
	manquantes := importation.EntetesManquantes(entetes, entetesObligatoiresImportVlans)
	if len(manquantes) > 0 {
		return importation.Rapport{
			Erreurs: []importation.ErreurLigne{{
				Ligne:   1,
				Message: fmt.Sprintf("colonne(s) obligatoire(s) manquante(s) : %s", strings.Join(manquantes, ", ")),
			}},
		}, nil, nil
	}

	var erreurs []importation.ErreurLigne
	var ordre []string
	parCode := map[string]*vlanImport{}
	cacheClusters := map[string]int64{}

	for i, ligne := range lignes {
		numero := numeros[i]
		var messages []string

		code := strings.TrimSpace(ligne["code"])
		if code == "" {
			erreurs = append(erreurs, importation.ErreurLigne{Ligne: numero, Message: "code vide"})
			continue
		}

		v, deja := parCode[code]
		if !deja {
			v = &vlanImport{NumeroLigne: numero, Vlan: depot.Vlan{Code: code}, criteres: criteresTexte(ligne)}
			// existence en base : refusée, l'import crée sans mettre à jour
			_, err := d.LireVlanParCode(code)
			switch {
			case err == nil:
				messages = append(messages, fmt.Sprintf("VLAN « %s » existe déjà", code))
			case errors.Is(err, depot.ErrIntrouvable):
			default:
				return importation.Rapport{}, nil, err
			}
			// critères : chacun vide = tous
			resoudre := func(col string, lire func(string) (int64, error), libelle string) *int64 {
				valeur := strings.TrimSpace(ligne[col])
				if valeur == "" {
					return nil
				}
				id, err := lire(valeur)
				if err != nil {
					messages = append(messages, fmt.Sprintf("%s « %s » inconnu", libelle, valeur))
					return nil
				}
				return &id
			}
			v.Vlan.ProjetID = resoudre("projet_code", func(c string) (int64, error) {
				p, err := d.LireProjetParCode(c)
				return p.ID, err
			}, "projet")
			v.Vlan.EnvironnementID = resoudre("environnement_code", func(c string) (int64, error) {
				e, err := d.LireEnvironnementParCode(c)
				return e.ID, err
			}, "environnement")
			v.Vlan.ZoneID = resoudre("zone_code", func(c string) (int64, error) {
				z, err := d.LireZoneParCode(c)
				return z.ID, err
			}, "zone")
			if nom := strings.TrimSpace(ligne["cluster_nom"]); nom != "" {
				id, err := resoudreClusterVlanImport(d, ligne["projet_code"], ligne["environnement_code"], nom, cacheClusters)
				var em erreurLigneMetier
				switch {
				case errors.As(err, &em):
					messages = append(messages, em.Error())
				case err != nil:
					return importation.Rapport{}, nil, err
				default:
					v.Vlan.ClusterID = &id
				}
			}
			v.Vlan.Commentaire = texteOptionnelCSV(ligne["commentaire"])
			parCode[code] = v
			ordre = append(ordre, code)
		} else if criteresTexte(ligne) != v.criteres {
			messages = append(messages, fmt.Sprintf(
				"VLAN « %s » : critères différents de la ligne %d (un VLAN a les mêmes critères sur toutes ses lignes)",
				code, v.NumeroLigne))
		} else if v.Vlan.Commentaire == nil {
			v.Vlan.Commentaire = texteOptionnelCSV(ligne["commentaire"])
		}

		// plage : les deux bornes, ou aucune
		debut, fin := strings.TrimSpace(ligne["ip_debut"]), strings.TrimSpace(ligne["ip_fin"])
		switch {
		case debut == "" && fin == "":
			// VLAN sans plage sur cette ligne
		case debut == "" || fin == "":
			messages = append(messages, "ip_debut et ip_fin doivent être renseignés ensemble, ou aucun")
		default:
			it, err := depot.ParserIntervalleIPv4(debut, fin)
			if err != nil {
				messages = append(messages, strings.TrimPrefix(err.Error(), depot.ErrValidation.Error()+" : "))
			} else {
				chevauche := false
				for _, autre := range v.intervalles {
					if it.Chevauche(autre) {
						chevauche = true
						break
					}
				}
				if chevauche {
					messages = append(messages, fmt.Sprintf("plage %s–%s : chevauchement avec une autre plage du VLAN « %s » dans le fichier",
						depot.FormaterIPv4(it.Debut), depot.FormaterIPv4(it.Fin), code))
				} else {
					v.intervalles = append(v.intervalles, it)
					v.Plages = append(v.Plages, depot.PlageIP{
						IPDebut: depot.FormaterIPv4(it.Debut), IPFin: depot.FormaterIPv4(it.Fin),
					})
				}
			}
		}

		if len(messages) > 0 {
			erreurs = append(erreurs, importation.ErreurLigne{Ligne: numero, Message: strings.Join(messages, " ; ")})
		}
	}

	valides := make([]vlanImport, 0, len(ordre))
	for _, code := range ordre {
		valides = append(valides, *parCode[code])
	}
	return importation.Rapport{NbLignes: len(lignes), Erreurs: erreurs}, valides, nil
}

// resoudreClusterVlanImport résout un cluster par (projet, environnement,
// nom) — les trois viennent de la ligne, le projet et l'environnement
// étant alors obligatoires. Même règle que l'import serveurs.
func resoudreClusterVlanImport(d *depot.Depot, projetCode, environnementCode, nom string, cache map[string]int64) (int64, error) {
	projetCode, environnementCode = strings.TrimSpace(projetCode), strings.TrimSpace(environnementCode)
	if projetCode == "" || environnementCode == "" {
		return 0, erreurLigneMetier{"cluster_nom exige projet_code et environnement_code sur la même ligne"}
	}
	cle := projetCode + "\x00" + environnementCode + "\x00" + nom
	if id, ok := cache[cle]; ok {
		return id, nil
	}
	var id int64
	err := d.Base().QueryRow(
		`SELECT c.id FROM cluster c
		 JOIN projet p ON p.id = c.projet_id
		 JOIN environnement e ON e.id = c.environnement_id
		 WHERE p.code = ? AND e.code = ? AND c.nom = ?`,
		projetCode, environnementCode, nom).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, erreurLigneMetier{fmt.Sprintf(
			"cluster « %s » inconnu pour projet_code « %s », environnement_code « %s »",
			nom, projetCode, environnementCode)}
	}
	if err != nil {
		return 0, err
	}
	cache[cle] = id
	return id, nil
}

// ---------------------------------------------------------------- écriture

// executerImportVlans écrit VLAN et plages dans une transaction unique sur
// Depot.Base() — les méthodes du dépôt ouvrent chacune la leur, ce qui ne
// permettrait pas « aucun import partiel ». Renvoie (nb VLAN, nb plages).
func executerImportVlans(d *depot.Depot, valides []vlanImport) (int, int, error) {
	tx, err := d.Base().Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("ouverture de la transaction d'import des VLAN : %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	nbPlages := 0
	for _, v := range valides {
		res, errExec := tx.Exec(
			`INSERT INTO vlan (code, projet_id, environnement_id, zone_id, cluster_id, commentaire)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			v.Vlan.Code, v.Vlan.ProjetID, v.Vlan.EnvironnementID, v.Vlan.ZoneID, v.Vlan.ClusterID, v.Vlan.Commentaire)
		if errExec != nil {
			err = fmt.Errorf("insertion du VLAN « %s » (ligne %d) : %w", v.Vlan.Code, v.NumeroLigne, errExec)
			return 0, 0, err
		}
		vlanID, _ := res.LastInsertId()
		if err = d.JournaliserCreation(tx, "vlan", "vlan", vlanID); err != nil {
			return 0, 0, err
		}
		for _, p := range v.Plages {
			resPlage, errExec := tx.Exec(`INSERT INTO plage_ip (vlan_id, ip_debut, ip_fin) VALUES (?, ?, ?)`,
				vlanID, p.IPDebut, p.IPFin)
			if errExec != nil {
				err = fmt.Errorf("insertion de la plage %s–%s du VLAN « %s » : %w", p.IPDebut, p.IPFin, v.Vlan.Code, errExec)
				return 0, 0, err
			}
			plageID, _ := resPlage.LastInsertId()
			if err = d.JournaliserCreation(tx, "plage_ip", "plage_ip", plageID); err != nil {
				return 0, 0, err
			}
			nbPlages++
		}
	}
	if errCommit := tx.Commit(); errCommit != nil {
		err = fmt.Errorf("validation de la transaction d'import des VLAN : %w", errCommit)
		return 0, 0, err
	}
	return len(valides), nbPlages, nil
}

// ---------------------------------------------------------------- écran

// rapportImportVlans est le contexte du gabarit "import_vlans_resultat" —
// mêmes états que rapportImportReferentiels.
type rapportImportVlans struct {
	MessageJeton string
	NbLignes     int
	Erreurs      []importation.ErreurLigne
	Jeton        string
	NbVlans      int
	NbPlages     int
	Ecrit        bool
}

func (s *serveur) analyserFichierVlans(contenu []byte) (importation.Rapport, []vlanImport, error) {
	entetes, lignes, numeros, err := importation.LireCSV(bytes.NewReader(contenu))
	if err != nil {
		return importation.Rapport{Erreurs: []importation.ErreurLigne{{Ligne: 0, Message: err.Error()}}}, nil, nil
	}
	return validerLignesVlans(s.depot, entetes, lignes, numeros)
}

func compterPlages(valides []vlanImport) int {
	n := 0
	for _, v := range valides {
		n += len(v.Plages)
	}
	return n
}

func (s *serveur) importVlansAnalyser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	fichier, _, err := r.FormFile("fichier")
	if err != nil {
		s.rendreFragment(w, r, "import_vlans_resultat", rapportImportVlans{
			Erreurs: []importation.ErreurLigne{{Ligne: 0, Message: "aucun fichier reçu"}},
		})
		return
	}
	defer fichier.Close()

	contenu, err := io.ReadAll(fichier)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	rapport, valides, err := s.analyserFichierVlans(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_vlans_resultat", rapportImportVlans{
			NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs,
		})
		return
	}
	jeton, err := importsEnAttente.Deposer(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_vlans_resultat", rapportImportVlans{
		NbLignes: rapport.NbLignes, Jeton: jeton, NbVlans: len(valides), NbPlages: compterPlages(valides),
	})
}

func (s *serveur) importVlansConfirmer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	jeton := r.FormValue("jeton")
	contenu, ok := importsEnAttente.Recuperer(jeton)
	if !ok {
		s.rendreFragment(w, r, "import_vlans_resultat", rapportImportVlans{
			MessageJeton: "Cet import a expiré ou a déjà été traité — réanalysez le fichier.",
		})
		return
	}
	defer importsEnAttente.Consommer(jeton)

	// revalidation : la base a pu changer entre l'analyse et la confirmation.
	rapport, valides, err := s.analyserFichierVlans(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_vlans_resultat", rapportImportVlans{
			NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs,
		})
		return
	}
	nbVlans, nbPlages, err := executerImportVlans(s.depotPour(r), valides)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_vlans_resultat", rapportImportVlans{
		NbLignes: rapport.NbLignes, Ecrit: true, NbVlans: nbVlans, NbPlages: nbPlages,
	})
}
