// Package capacity implémente le moteur de calcul de capacité de Parallax :
// évaluation des formules, résolution des variables, application des règles
// et dimensionnement sous contraintes.
//
// Ce package n'a AUCUNE dépendance externe, volontairement : il est testable
// hors ligne et sa sémantique numérique est entièrement sous notre contrôle.
// Les formules produisent des chiffres de budget ; on ne délègue pas leur
// arithmétique à une bibliothèque généraliste dont les règles de conversion
// pourraient dériver d'une version à l'autre.
//
// La sémantique de référence est définie par spec/capacity_reference.py et
// vérifiée par les vecteurs de spec/vectors.json.
package capacity

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Grammaire :
//
//	expression := terme (('+' | '-') terme)*
//	terme      := unaire (('*' | '/' | '%') unaire)*
//	unaire     := ('+' | '-') unaire | puissance
//	puissance  := primaire ('^' unaire)?          // associatif à droite
//	primaire   := NOMBRE | IDENT | IDENT '(' args ')' | '(' expression ')'

type tokenKind int

const (
	tkEOF tokenKind = iota
	tkNombre
	tkIdent
	tkOperateur
	tkParenOuvrante
	tkParenFermante
	tkVirgule
)

type token struct {
	kind   tokenKind
	texte  string
	nombre float64
	pos    int
}

func lexer(source string) ([]token, error) {
	var tokens []token
	runes := []rune(source)
	i := 0
	for i < len(runes) {
		c := runes[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case unicode.IsDigit(c) || c == '.':
			debut := i
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.') {
				i++
			}
			texte := string(runes[debut:i])
			valeur, err := strconv.ParseFloat(texte, 64)
			if err != nil {
				return nil, fmt.Errorf("nombre invalide « %s » en position %d", texte, debut)
			}
			tokens = append(tokens, token{kind: tkNombre, texte: texte, nombre: valeur, pos: debut})
		case unicode.IsLetter(c) || c == '_':
			debut := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			tokens = append(tokens, token{kind: tkIdent, texte: string(runes[debut:i]), pos: debut})
		case strings.ContainsRune("+-*/%^", c):
			tokens = append(tokens, token{kind: tkOperateur, texte: string(c), pos: i})
			i++
		case c == '(':
			tokens = append(tokens, token{kind: tkParenOuvrante, texte: "(", pos: i})
			i++
		case c == ')':
			tokens = append(tokens, token{kind: tkParenFermante, texte: ")", pos: i})
			i++
		case c == ',':
			tokens = append(tokens, token{kind: tkVirgule, texte: ",", pos: i})
			i++
		default:
			return nil, fmt.Errorf("caractère inattendu « %c » en position %d", c, i)
		}
	}
	return append(tokens, token{kind: tkEOF, pos: len(runes)}), nil
}

// Noeud est un élément de l'arbre d'une expression compilée.
type Noeud interface {
	eval(env map[string]float64) (float64, error)
	collecter(dans map[string]struct{})
}

type noeudNombre struct{ valeur float64 }

func (n noeudNombre) eval(map[string]float64) (float64, error) { return n.valeur, nil }
func (n noeudNombre) collecter(map[string]struct{})            {}

type noeudVariable struct{ nom string }

func (n noeudVariable) eval(env map[string]float64) (float64, error) {
	v, ok := env[n.nom]
	if !ok {
		return 0, fmt.Errorf("variable inconnue : %s", n.nom)
	}
	return v, nil
}
func (n noeudVariable) collecter(dans map[string]struct{}) { dans[n.nom] = struct{}{} }

type noeudBinaire struct {
	op     string
	gauche Noeud
	droite Noeud
}

func (n noeudBinaire) eval(env map[string]float64) (float64, error) {
	g, err := n.gauche.eval(env)
	if err != nil {
		return 0, err
	}
	d, err := n.droite.eval(env)
	if err != nil {
		return 0, err
	}
	switch n.op {
	case "+":
		return g + d, nil
	case "-":
		return g - d, nil
	case "*":
		return g * d, nil
	case "/":
		if d == 0 {
			return 0, errDivisionParZero
		}
		return g / d, nil
	case "%":
		if d == 0 {
			return 0, errDivisionParZero
		}
		return math.Mod(g, d), nil
	case "^":
		return math.Pow(g, d), nil
	}
	return 0, fmt.Errorf("opérateur inconnu : %s", n.op)
}
func (n noeudBinaire) collecter(dans map[string]struct{}) {
	n.gauche.collecter(dans)
	n.droite.collecter(dans)
}

type noeudUnaire struct {
	op       string
	operande Noeud
}

func (n noeudUnaire) eval(env map[string]float64) (float64, error) {
	v, err := n.operande.eval(env)
	if err != nil {
		return 0, err
	}
	if n.op == "-" {
		return -v, nil
	}
	return v, nil
}
func (n noeudUnaire) collecter(dans map[string]struct{}) { n.operande.collecter(dans) }

type noeudAppel struct {
	nom       string
	arguments []Noeud
}

func (n noeudAppel) eval(env map[string]float64) (float64, error) {
	valeurs := make([]float64, len(n.arguments))
	for i, a := range n.arguments {
		v, err := a.eval(env)
		if err != nil {
			return 0, err
		}
		valeurs[i] = v
	}
	return appliquerFonction(n.nom, valeurs)
}
func (n noeudAppel) collecter(dans map[string]struct{}) {
	for _, a := range n.arguments {
		a.collecter(dans)
	}
}

var errDivisionParZero = fmt.Errorf("division par zéro")

// arrondi arrondit au plus loin de zéro sur les demis (2,5 -> 3).
// La spécification Python impose la même règle, Python arrondissant
// nativement au pair le plus proche.
func arrondi(x float64) float64 {
	if x >= 0 {
		return math.Floor(x + 0.5)
	}
	return math.Ceil(x - 0.5)
}

// FonctionsDisponibles liste les fonctions utilisables dans une formule,
// pour affichage dans l'aide à la saisie.
var FonctionsDisponibles = []string{"abs", "ceil", "floor", "max", "min", "round"}

func appliquerFonction(nom string, args []float64) (float64, error) {
	switch nom {
	case "ceil", "floor", "round", "abs":
		if len(args) != 1 {
			return 0, fmt.Errorf("%s attend un argument, %d fournis", nom, len(args))
		}
		switch nom {
		case "ceil":
			return math.Ceil(args[0]), nil
		case "floor":
			return math.Floor(args[0]), nil
		case "round":
			return arrondi(args[0]), nil
		default:
			return math.Abs(args[0]), nil
		}
	case "min", "max":
		if len(args) == 0 {
			return 0, fmt.Errorf("%s attend au moins un argument", nom)
		}
		r := args[0]
		for _, v := range args[1:] {
			if (nom == "min" && v < r) || (nom == "max" && v > r) {
				r = v
			}
		}
		return r, nil
	}
	return 0, fmt.Errorf("fonction non autorisée : %s", nom)
}

type analyseur struct {
	tokens []token
	i      int
}

func (a *analyseur) courant() token { return a.tokens[a.i] }
func (a *analyseur) avancer() token { t := a.tokens[a.i]; a.i++; return t }
func (a *analyseur) estOp(ops ...string) bool {
	t := a.courant()
	if t.kind != tkOperateur {
		return false
	}
	for _, op := range ops {
		if t.texte == op {
			return true
		}
	}
	return false
}

func (a *analyseur) expression() (Noeud, error) {
	gauche, err := a.terme()
	if err != nil {
		return nil, err
	}
	for a.estOp("+", "-") {
		op := a.avancer().texte
		droite, err := a.terme()
		if err != nil {
			return nil, err
		}
		gauche = noeudBinaire{op: op, gauche: gauche, droite: droite}
	}
	return gauche, nil
}

func (a *analyseur) terme() (Noeud, error) {
	gauche, err := a.unaire()
	if err != nil {
		return nil, err
	}
	for a.estOp("*", "/", "%") {
		op := a.avancer().texte
		droite, err := a.unaire()
		if err != nil {
			return nil, err
		}
		gauche = noeudBinaire{op: op, gauche: gauche, droite: droite}
	}
	return gauche, nil
}

func (a *analyseur) unaire() (Noeud, error) {
	if a.estOp("+", "-") {
		op := a.avancer().texte
		operande, err := a.unaire()
		if err != nil {
			return nil, err
		}
		return noeudUnaire{op: op, operande: operande}, nil
	}
	return a.puissance()
}

func (a *analyseur) puissance() (Noeud, error) {
	base, err := a.primaire()
	if err != nil {
		return nil, err
	}
	if a.estOp("^") {
		a.avancer()
		exposant, err := a.unaire() // associatif à droite
		if err != nil {
			return nil, err
		}
		return noeudBinaire{op: "^", gauche: base, droite: exposant}, nil
	}
	return base, nil
}

func (a *analyseur) primaire() (Noeud, error) {
	t := a.avancer()
	switch t.kind {
	case tkNombre:
		return noeudNombre{valeur: t.nombre}, nil

	case tkIdent:
		if a.courant().kind != tkParenOuvrante {
			return noeudVariable{nom: t.texte}, nil
		}
		a.avancer() // consomme '('
		var args []Noeud
		if a.courant().kind != tkParenFermante {
			for {
				arg, err := a.expression()
				if err != nil {
					return nil, err
				}
				args = append(args, arg)
				if a.courant().kind != tkVirgule {
					break
				}
				a.avancer()
			}
		}
		if a.courant().kind != tkParenFermante {
			return nil, fmt.Errorf("parenthèse fermante attendue en position %d", a.courant().pos)
		}
		a.avancer()
		if !estFonctionConnue(t.texte) {
			return nil, fmt.Errorf("fonction non autorisée : %s", t.texte)
		}
		return noeudAppel{nom: t.texte, arguments: args}, nil

	case tkParenOuvrante:
		interne, err := a.expression()
		if err != nil {
			return nil, err
		}
		if a.courant().kind != tkParenFermante {
			return nil, fmt.Errorf("parenthèse fermante attendue en position %d", a.courant().pos)
		}
		a.avancer()
		return interne, nil

	case tkEOF:
		return nil, fmt.Errorf("expression incomplète")
	}
	return nil, fmt.Errorf("élément inattendu « %s » en position %d", t.texte, t.pos)
}

func estFonctionConnue(nom string) bool {
	for _, f := range FonctionsDisponibles {
		if f == nom {
			return true
		}
	}
	return false
}

// Expression est une formule compilée, réutilisable et sûre à évaluer.
type Expression struct {
	Source string
	racine Noeud
}

// Compiler analyse une formule. Les erreurs de syntaxe et les fonctions
// inconnues sont détectées ici, à la saisie de la règle, et non à l'exécution.
func Compiler(source string) (*Expression, error) {
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("expression vide")
	}
	tokens, err := lexer(source)
	if err != nil {
		return nil, err
	}
	a := &analyseur{tokens: tokens}
	racine, err := a.expression()
	if err != nil {
		return nil, err
	}
	if a.courant().kind != tkEOF {
		return nil, fmt.Errorf("élément inattendu « %s » en position %d",
			a.courant().texte, a.courant().pos)
	}
	return &Expression{Source: source, racine: racine}, nil
}

// Evaluer calcule la valeur de l'expression dans un environnement donné.
func (e *Expression) Evaluer(env map[string]float64) (float64, error) {
	v, err := e.racine.eval(env)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("résultat non fini")
	}
	return v, nil
}

// Variables retourne les identifiants référencés par l'expression, triés.
// Sert à vérifier qu'une règle ne référence que des variables déclarées.
func (e *Expression) Variables() []string {
	dans := map[string]struct{}{}
	e.racine.collecter(dans)
	out := make([]string, 0, len(dans))
	for nom := range dans {
		out = append(out, nom)
	}
	sort.Strings(out)
	return out
}

// Evaluer compile puis évalue en une fois. À réserver aux usages ponctuels :
// pour un calcul répété, compiler une seule fois.
func Evaluer(source string, env map[string]float64) (float64, error) {
	e, err := Compiler(source)
	if err != nil {
		return 0, err
	}
	return e.Evaluer(env)
}
