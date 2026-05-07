package cmd

import "github.com/spf13/cobra"

// registerCommands builds the verb-first command tree on the given root.
// Each verb (list/create/delete/show/clone/attach) is a bare grouper
// hosting its subject commands; run and template are root-level verbs.
func registerCommands(root *cobra.Command) {
	listCmd := &cobra.Command{Use: "list", Short: "List resources"}
	createCmd := &cobra.Command{Use: "create", Short: "Create resources"}
	deleteCmd := &cobra.Command{Use: "delete", Short: "Delete resources"}
	showCmd := &cobra.Command{Use: "show", Short: "Show details for a resource"}
	cloneCmd := &cobra.Command{Use: "clone", Short: "Clone a project from remote"}
	attachCmd := &cobra.Command{Use: "attach", Short: "Attach to a running session"}

	listCmd.AddCommand((&ListProjectCmd{}).Cobra())
	listCmd.AddCommand((&ListWorktreeCmd{}).Cobra())
	listCmd.AddCommand((&ListSessionCmd{}).Cobra())
	showCmd.AddCommand((&ShowProjectCmd{}).Cobra())
	showCmd.AddCommand((&ShowSessionCmd{}).Cobra())
	cloneCmd.AddCommand((&CloneProjectCmd{}).Cobra())
	createCmd.AddCommand((&CreateWorktreeCmd{}).Cobra())
	createCmd.AddCommand((&CreateSessionCmd{}).Cobra())
	deleteCmd.AddCommand((&DeleteWorktreeCmd{}).Cobra())
	deleteCmd.AddCommand((&DeleteSessionCmd{}).Cobra())
	attachCmd.AddCommand((&AttachSessionCmd{}).Cobra())

	root.AddCommand(listCmd, createCmd, deleteCmd, showCmd, cloneCmd, attachCmd)

	runCmd := &RunAgentCmd{}
	root.AddCommand(runCmd.Cobra())

	root.AddCommand((&TemplateCmd{}).Cobra())
}
