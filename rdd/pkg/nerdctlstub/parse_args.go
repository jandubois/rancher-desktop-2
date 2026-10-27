// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package nerdctlstub rewrites the host paths in a nerdctl command line so
// that they work when nerdctl runs inside the guest VM. It rewrites the
// values of the flags that the overlay lists. The
// parser is ported from Rancher Desktop 1.x (src/go/nerdctl-stub); the
// command table is generated from the pinned nerdctl module by ./generate.
package nerdctlstub

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sirupsen/logrus"
)

// errUnknownOption marks an option that the parser matches neither at a
// command nor at any command enclosing it.
var errUnknownOption = errors.New("unknown option")

// errMissingValue marks an option that takes its value from the next
// argument but is the last argument on the command line.
var errMissingValue = errors.New("missing value for option")

// argHandler is the type of a function that rewrites one argument value.
type argHandler func(string) (string, error)

type commandDefinition struct {
	// commandPath is the arguments needed to reach this command.
	commandPath string
	// subcommands that can be spawned from this command.
	subcommands map[string]struct{}
	// options for this (sub)command. If the handler is nil, the option
	// does not take an argument.
	options map[string]argHandler
	// hasForeignFlags means option parsing stops at the first positional
	// argument, because later flags are arguments, such as the container
	// command's in `nerdctl run image ls --color`.
	hasForeignFlags bool
}

// lookupCommand finds a command definition by path, resolving aliases.
func lookupCommand(path string) (commandDefinition, bool) {
	if def, ok := commands[path]; ok {
		return def, true
	}
	if canonical, ok := commandAliases[path]; ok {
		if def, ok := commands[canonical]; ok {
			return def, true
		}
	}
	return commandDefinition{}, false
}

// parent returns the command that encloses this one; ok is false at the
// root.
func (c *commandDefinition) parent() (parent commandDefinition, ok bool) {
	if c.commandPath == "" {
		return commandDefinition{}, false
	}
	parentPath := ""
	if lastSpace := strings.LastIndex(c.commandPath, " "); lastSpace > -1 {
		parentPath = c.commandPath[:lastSpace]
	}
	parent, ok = lookupCommand(parentPath)
	if !ok {
		panic(fmt.Sprintf("command %q could not find parent %q", c.commandPath, parentPath))
	}
	return parent, true
}

// lookupOption finds the handler of an option at this command or at the
// nearest enclosing command that lists it.
func (c *commandDefinition) lookupOption(option string) (argHandler, bool) {
	for def, ok := *c, true; ok; def, ok = def.parent() {
		if handler, found := def.options[option]; found {
			return handler, true
		}
	}
	return nil, false
}

// parseOption takes an argument (that is known to start with `-` or `--`)
// plus the next argument (which may be needed if a value is required), and
// returns the rewritten arguments and whether the value argument was
// consumed. hasNext is false when arg ends the command line.
func (c *commandDefinition) parseOption(arg, next string, hasNext bool) (newArgs []string, consumedNext bool, err error) {
	if !strings.HasPrefix(arg, "-") {
		panic(fmt.Sprintf("commandDefinition.parseOption called with invalid arg %q", arg))
	}

	option := arg
	value := next
	consumed := true
	handler, ok := c.options[option]
	if !ok && len(arg) > 1 && arg[1] != '-' {
		// There may be single-character options bunched together, with the
		// last one taking a value, e.g. `-itp 80` or `-itp80`.
		option, value, consumed, handler, ok = c.parseShortOptions(arg, next)
	}
	if !ok {
		option = arg
		value = next
		consumed = true
		if sep := strings.Index(option, "="); sep >= 0 {
			value = option[sep+1:]
			option = option[:sep]
			consumed = false
		}
		handler, ok = c.options[option]
	}
	if ok {
		if handler == nil {
			// This does not consume a value, and needs no rewriting.
			return []string{arg}, false, nil
		}
		if consumed && !hasNext {
			return nil, false, fmt.Errorf("%w %s", errMissingValue, arg)
		}
		converted, err := handler(value)
		if err != nil {
			return nil, consumed, err
		}
		if !consumed && converted == value {
			// Keep an attached value attached. With TraverseChildren set on
			// nerdctl's root, cobra reads a separate value such as `--certs` as
			// a flag and misses the subcommand that follows.
			return []string{arg}, false, nil
		}
		return []string{option, converted}, consumed, nil
	}

	// Check if the parent command can resolve a long option. parseShortOptions
	// already looked up each letter of a single-dash bunch at every level. A
	// retry at the parent would skip this command's own flags, so it could
	// only misread the bunch.
	if parent, ok := c.parent(); ok && strings.HasPrefix(arg, "--") {
		parentResult, parentConsumed, parentErr := parent.parseOption(arg, next, hasNext)
		if !errors.Is(parentErr, errUnknownOption) {
			// An enclosing command knows the option; return its result or
			// its handler's error.
			return parentResult, parentConsumed, parentErr
		}
	}
	return nil, false, fmt.Errorf("%w %s for command %q", errUnknownOption, arg, c.commandPath)
}

// parseShortOptions walks a single-dash argument the way pflag does. The
// first letter that takes a value gets the rest of the argument, or the next
// argument when nothing follows, and the returned option ends at that letter.
// A boolean letter gets the rest only after `=`, as in `-dt=false`, and then
// the whole argument is the option. A `=` right after a letter that takes a
// value is dropped only when text follows it, so `-e=` has the value `=`.
//
// A letter may belong to an enclosing command, as the `p` of `compose run
// -dp proj` belongs to compose. The table does not say which flags cobra
// passes down, so a letter may also match a flag that nerdctl rejects in a
// subcommand; nerdctl then reports it. ok is false when a letter is unknown
// at every level.
func (c *commandDefinition) parseShortOptions(arg, next string) (option, value string, consumed bool, handler argHandler, ok bool) {
	for i := 1; i < len(arg); i++ {
		handler, ok = c.lookupOption("-" + arg[i:i+1])
		if !ok {
			return "", "", false, nil, false
		}
		rest := arg[i+1:]
		hasEqualsValue := len(rest) > 1 && rest[0] == '='
		if handler == nil {
			if hasEqualsValue {
				// The boolean takes the rest; nothing needs rewriting.
				return arg, "", false, nil, true
			}
			continue
		}
		switch {
		case hasEqualsValue:
			return arg[:i+1], rest[1:], false, handler, true
		case rest != "":
			return arg[:i+1], rest, false, handler, true
		default:
			return arg[:i+1], next, true, handler, true
		}
	}
	// Every letter is a boolean option.
	return arg, "", false, nil, true
}

// parse arguments for this command; this includes options (--long, -x) as
// well as subcommands and positional arguments.
func (c commandDefinition) parse(args []string) ([]string, error) {
	// Parsing rules:
	// - At each command level, options (-x, -dp or --long) of that level or
	//   of any enclosing level can be parsed, and the letters of one bunch
	//   (-dp) may come from different levels.
	// - Positional arguments can be intermixed with (long and short) options.
	// - If a command has foreign flags (e.g. `nerdctl run`), option parsing
	//   stops at the first positional argument. This means we parse the flag
	//   in `nerdctl run --env foo=bar image sh -c ...` but not the `--env`
	//   flag in `nerdctl run image --env foo=bar sh -c ...`.
	// - `--` stops parsing of options.
	// - An unknown option stops parsing; it and the arguments after it pass
	//   through unchanged.
	var result []string
	var positionalArgs []string
	var unparsedArgs []string
argLoop:
	for argIndex := 0; argIndex < len(args); argIndex++ {
		arg := args[argIndex]
		switch {
		case arg == "--":
			// No more options, only positional arguments.
			positionalArgs = append(positionalArgs, args[argIndex+1:]...)
			break argLoop
		case strings.HasPrefix(arg, "-") && arg != "-":
			// pflag takes a lone `-` as a positional argument.
			hasNext := argIndex+1 < len(args)
			next := ""
			if hasNext {
				next = args[argIndex+1]
			}
			newArgs, consumed, err := c.parseOption(arg, next, hasNext)
			if errors.Is(err, errUnknownOption) || errors.Is(err, errMissingValue) {
				// The stub cannot tell whether an unknown option takes a
				// value, so any later argument might be that value, and
				// rewriting one could corrupt it. nerdctl reports the option
				// if it is wrong. nerdctl always reports a missing value.
				logrus.Debugf("%s; passing it and later arguments through unchanged", err)
				unparsedArgs = args[argIndex:]
				break argLoop
			}
			if err != nil {
				return nil, err
			}
			result = append(result, newArgs...)
			if consumed {
				argIndex++
			}
		case len(c.subcommands) > 0:
			// This command has subcommands; assume any non-flag argument is
			// a subcommand and hand off parsing to it.
			subcommandPath := c.commandPath
			if subcommandPath != "" {
				subcommandPath += " "
			}
			subcommandPath += arg
			if subcommand, ok := lookupCommand(subcommandPath); ok {
				childResult, err := subcommand.parse(args[argIndex+1:])
				if err != nil {
					return nil, err
				}
				result = append(result, arg)
				result = append(result, childResult...)
			} else {
				// Unknown subcommand (e.g. `nerdctl internal`); pass all
				// remaining arguments through unmodified.
				result = append(result, args[argIndex:]...)
			}
			break argLoop
		case c.hasForeignFlags:
			// The rest of the arguments starting from the first positional
			// argument is foreign.
			positionalArgs = append(positionalArgs, args[argIndex:]...)
			break argLoop
		default:
			// This command has neither subcommands nor foreign arguments;
			// everything else is positional, but later flags still parse.
			positionalArgs = append(positionalArgs, arg)
		}
	}
	// At this point, `result` holds the options, and `positionalArgs` the
	// unparsed positional arguments. A `--` would turn unparsed options into
	// positional arguments.
	if len(positionalArgs) > 0 && len(unparsedArgs) == 0 {
		result = slices.Concat(result, []string{"--"}, positionalArgs)
	} else {
		result = append(result, positionalArgs...)
	}
	result = append(result, unparsedArgs...)
	return result, nil
}

// TranslateCommandLine parses a nerdctl command line (excluding the program
// name) and returns it with guest paths in place of the host paths that the
// stub recognizes.
func TranslateCommandLine(args []string) ([]string, error) {
	root, ok := lookupCommand("")
	if !ok {
		panic("no root command definition")
	}
	return root.parse(args)
}

// ignoredArgHandler handles arguments that do not contain paths.
func ignoredArgHandler(input string) (string, error) {
	return input, nil
}

// kindHandlers maps the overlay classification to the rewrite functions.
func kindHandlers() map[argHandlerKind]argHandler {
	return map[argHandlerKind]argHandler{
		filePathArg:         filePathArgHandler,
		outputPathArg:       outputPathArgHandler,
		volumeArg:           volumeArgHandler,
		mountArg:            mountArgHandler,
		fileOrURLOrStdinArg: fileOrURLOrStdinArgHandler,
		filePathListArg:     filePathListArgHandler,
	}
}

// commands is the runtime command table, built from the generated
// commandSpecs plus the pathFlags overlay.
var commands = map[string]commandDefinition{}

func init() {
	for path, spec := range commandSpecs {
		def := commandDefinition{
			commandPath:     path,
			subcommands:     map[string]struct{}{},
			options:         map[string]argHandler{},
			hasForeignFlags: spec.foreignFlags,
		}
		for _, name := range spec.subcommands {
			def.subcommands[name] = struct{}{}
		}
		for _, name := range spec.valueFlags {
			def.options[name] = ignoredArgHandler
		}
		for _, name := range spec.boolFlags {
			def.options[name] = nil
		}
		commands[path] = def
	}

	// Apply the path-flag overlay.
	handlers := kindHandlers()
	for path, flags := range pathFlags {
		def, ok := commands[path]
		if !ok {
			panic(fmt.Sprintf("pathFlags: unknown command %q", path))
		}
		for name, kind := range flags {
			if _, ok := def.options[name]; !ok {
				panic(fmt.Sprintf("pathFlags: command %q has no option %q", path, name))
			}
			def.options[name] = handlers[kind]
		}
	}
}
