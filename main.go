package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apiserver/pkg/authentication/request/bearertoken"
	"k8s.io/apiserver/pkg/server"
	apiserverflag "k8s.io/apiserver/pkg/util/flag"
	"k8s.io/apiserver/pkg/util/globalflag"
	"k8s.io/apiserver/plugin/pkg/authenticator/token/oidc"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	apiserveroptions "k8s.io/kubernetes/pkg/kubeapiserver/options"
	"k8s.io/kubernetes/pkg/version/verflag"
)

func main() {
	stopCh := make(chan struct{})
	cmd := newRunCommand(stopCh)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newRunCommand(stopCh <-chan struct{}) *cobra.Command {
	// flag option structs
	oidcOptions := &apiserveroptions.BuiltInAuthenticationOptions{
		OIDC: &apiserveroptions.OIDCAuthenticationOptions{},
	}
	secureServingOptions := apiserveroptions.NewSecureServingOptions()
	secureServingOptions.ServerCert.PairName = "kube-oidc-proxy"
	clientConfigFlags := genericclioptions.NewConfigFlags()

	// proxy command
	cmd := &cobra.Command{
		Use:  "k8s-oidc-proxy",
		Long: "k8s-oidc-proxy is a reverse proxy to authenticate users to Kubernetes API servers with Open ID Connect Authentication.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// client rest config
			restConfig, err := clientConfigFlags.ToRawKubeConfigLoader().ClientConfig()
			if err != nil {
				return err
			}

			// oidc config
			oidcAuther, err := oidc.New(oidc.Options{
				APIAudiences:         oidcOptions.APIAudiences,
				CAFile:               oidcOptions.OIDC.CAFile,
				ClientID:             oidcOptions.OIDC.ClientID,
				GroupsClaim:          oidcOptions.OIDC.GroupsClaim,
				GroupsPrefix:         oidcOptions.OIDC.GroupsPrefix,
				IssuerURL:            oidcOptions.OIDC.IssuerURL,
				RequiredClaims:       oidcOptions.OIDC.RequiredClaims,
				SupportedSigningAlgs: oidcOptions.OIDC.SigningAlgs,
				UsernameClaim:        oidcOptions.OIDC.UsernameClaim,
				UsernamePrefix:       oidcOptions.OIDC.UsernamePrefix,
			})
			if err != nil {
				return err
			}

			// oidc auther from config
			reqAuther := bearertoken.New(oidcAuther)
			//secure serving info has a Serve( function
			secureServingInfo := new(server.SecureServingInfo)
			if err := secureServingOptions.ApplyTo(&secureServingInfo, nil); err != nil {
				return err
			}

			p := &Proxy{
				restConfig:        restConfig,
				reqAuther:         reqAuther,
				secureServingInfo: secureServingInfo,
			}

			// run proxy
			if err := p.Run(stopCh); err != nil {
				return err
			}

			<-stopCh

			return nil
		},
	}

	// add flags to command sets
	var namedFlagSets apiserverflag.NamedFlagSets
	fs := cmd.Flags()

	oidcfs := namedFlagSets.FlagSet("OIDC")
	oidcOptions.AddFlags(oidcfs)

	secureServingOptions.AddFlags(namedFlagSets.FlagSet("secure serving"))

	clientConfigFlags.CacheDir = nil
	clientConfigFlags.Impersonate = nil
	clientConfigFlags.ImpersonateGroup = nil
	clientConfigFlags.AddFlags(namedFlagSets.FlagSet("client"))

	verflag.AddFlags(namedFlagSets.FlagSet("misc"))
	globalflag.AddGlobalFlags(namedFlagSets.FlagSet("misc"), cmd.Name())

	for _, f := range namedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	// pretty output from kube-apiserver
	usageFmt := "Usage:\n  %s\n"
	cols, _, _ := apiserverflag.TerminalSize(cmd.OutOrStdout())
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		fmt.Fprintf(cmd.OutOrStderr(), usageFmt, cmd.UseLine())
		apiserverflag.PrintSections(cmd.OutOrStderr(), namedFlagSets, cols)
		return nil
	})
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n"+usageFmt, cmd.Long, cmd.UseLine())
		apiserverflag.PrintSections(cmd.OutOrStdout(), namedFlagSets, cols)
	})

	return cmd
}
