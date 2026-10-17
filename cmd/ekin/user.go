package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"ekin/internal/app"
	identitydomain "ekin/internal/domain/identity/v1"
)

const userUsage = `usage:
  ekin user create --email <address> --name <name>
  ekin user reset-password --email <address>

The password is read from EKIN_PASSWORD, or prompted for on a terminal,
or read as one line from stdin.
`

// user runs the account commands. Accounts otherwise only come from
// invitations, so `user create` is how the first owner gets in.
func user(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, userUsage)
		return errors.New("missing user command")
	}
	fs := flag.NewFlagSet("user "+args[0], flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, userUsage) }
	rawEmail := fs.String("email", "", "account email")
	name := fs.String("name", "", "display name (create only)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	email, err := identitydomain.ParseEmail(*rawEmail)
	if err != nil {
		return err
	}

	switch args[0] {
	case "create":
		pw, err := readPassword()
		if err != nil {
			return err
		}
		first, err := a.Identity().HasUsers(ctx)
		if err != nil {
			return err
		}
		u, err := a.Identity().CreateUser(ctx, email, *name, pw)
		if err != nil {
			return err
		}
		fmt.Printf("created %s (%s)\n", u.Email(), u.ID())
		if !first {
			// Workspaces made before accounts existed have no members;
			// the first account owns them.
			n, err := a.Workspaces().AdoptOrphans(ctx, u.ID())
			if err != nil {
				return err
			}
			if n > 0 {
				fmt.Printf("owner of %d existing workspace(s)\n", n)
			}
		}
		return nil
	case "reset-password":
		pw, err := readPassword()
		if err != nil {
			return err
		}
		if err := a.Identity().ResetPassword(ctx, email, pw); err != nil {
			return err
		}
		fmt.Printf("password reset for %s; all sessions signed out\n", email)
		return nil
	}
	fmt.Fprint(os.Stderr, userUsage)
	return fmt.Errorf("unknown user command %q", args[0])
}

func readPassword() (identitydomain.Password, error) {
	if v, ok := os.LookupEnv("EKIN_PASSWORD"); ok {
		return identitydomain.ParsePassword(v)
	}
	fd := int(os.Stdin.Fd()) //nolint:gosec // stdin's descriptor fits an int
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return identitydomain.Password{}, err
		}
		return identitydomain.ParsePassword(strings.TrimRight(line, "\r\n"))
	}
	prompt := func(label string) (string, error) {
		fmt.Fprint(os.Stderr, label)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	first, err := prompt("Password: ")
	if err != nil {
		return identitydomain.Password{}, err
	}
	pw, err := identitydomain.ParsePassword(first)
	if err != nil {
		return identitydomain.Password{}, err
	}
	again, err := prompt("Again: ")
	if err != nil {
		return identitydomain.Password{}, err
	}
	if again != first {
		return identitydomain.Password{}, errors.New("passwords do not match")
	}
	return pw, nil
}
