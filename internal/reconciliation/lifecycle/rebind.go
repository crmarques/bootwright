package lifecycle

import (
	"context"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const RebindAuthorization = "rebind="

func splitRebinds(contextName string, verb reconciliation.Verb, authorizations []string) ([]string, []string, error) {
	var tokens, rebinds []string
	for _, token := range authorizations {
		secret, rebind := strings.CutPrefix(token, RebindAuthorization)
		if !rebind {
			tokens = append(tokens, token)
			continue
		}
		if !api.ValidLexical(api.LexicalName, secret) || slices.Contains(rebinds, secret) {
			return nil, nil, authorizeRebindInvalid.Fail(
				"--authorize "+token+" must name one Secret once",
				"repeat "+contextCommand(contextName, string(verb))+" naming each rotated access credential once as --authorize rebind=<secret>")
		}
		rebinds = append(rebinds, secret)
	}
	slices.Sort(rebinds)
	return tokens, rebinds, nil
}

func (s Service) admitRebinds(ctx context.Context, name string, decided operationPlan, rebinds []string) (operationPlan, error) {
	if len(rebinds) == 0 {
		return decided, nil
	}
	without := "repeat " + contextCommand(name, string(decided.verb)) + " without --authorize " + RebindAuthorization
	binding := frozenBinding(decided)
	if binding == "" {
		return operationPlan{}, authorizeRebindInvalid.Fail(
			"this "+string(decided.verb)+" reopens no Secret binding, so no Secret is rebound", without+rebinds[0])
	}
	bound, err := s.pinnedVersions(ctx, name, binding)
	if err != nil {
		return operationPlan{}, err
	}
	current, err := s.binder.ReadCurrent(ctx, custody.ReadCurrentRequest{ContextName: name, Names: rebinds})
	if err != nil {
		return operationPlan{}, err
	}
	currentVersions := map[string]string{}
	for _, item := range current {
		currentVersions[item.Version.Declaration.Name] = item.Version.ID
		item.Material.Clear()
	}
	for _, secret := range rebinds {
		pinned, held := bound[secret]
		switch {
		case !held:
			return operationPlan{}, authorizeRebindInvalid.Fail(
				"the Secret binding "+binding+" this "+string(decided.verb)+" reopens pins no version of Secret "+secret, without+secret)
		case pinned == currentVersions[secret]:
			return operationPlan{}, authorizeRebindInvalid.Fail(
				"Secret "+secret+" is already presented at its current version, so there is nothing to rebind", without+secret)
		}
		if err := s.presentedOnly(decided, secret, without+secret); err != nil {
			return operationPlan{}, err
		}
	}
	decided.rebinds = rebinds
	return decided, nil
}

func (s Service) pinnedVersions(ctx context.Context, name, binding string) (map[string]string, error) {
	reopened, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: binding})
	for _, item := range reopened {
		item.Material.Clear()
	}
	if err != nil {
		return nil, err
	}
	pinned := make(map[string]string, len(reopened))
	for _, item := range reopened {
		pinned[item.Version.Declaration.Name] = item.Version.ID
	}
	return pinned, nil
}

func (s Service) presentedOnly(decided operationPlan, secret, remedy string) error {
	var presented, refused []string
	for index, block := range decided.plan.Blocks {
		step := "step " + strconv.Itoa(index+1) + " (" + block.Description + ")"
		capability, used, err := s.referencing(block, step, secret, remedy)
		if err != nil {
			return err
		}
		if !used {
			continue
		}
		var classes secrets.Classes
		if classifier, ok := capability.(SecretClassifier); ok {
			if classes, err = classifier.SecretClasses(block); err != nil {
				return err
			}
		}
		if classes.Of(secret) == secrets.AccessCredential {
			presented = append(presented, step)
		} else {
			refused = append(refused, step)
		}
	}
	if len(refused) != 0 {
		return authorizeRebindInvalid.Fail(
			"Secret "+secret+" is not only an access credential this "+string(decided.verb)+" presents: "+strings.Join(refused, ", ")+
				" rest on the version its operation bound, which alone proves what they created", remedy)
	}
	if len(presented) == 0 {
		return authorizeRebindInvalid.Fail(
			"no step of this "+string(decided.verb)+" presents Secret "+secret, remedy)
	}
	return nil
}

func (s Service) referencing(block reconciliation.Block, step, secret, remedy string) (Capability, bool, error) {
	capability, found := s.capabilities.Resolve(block.Kind, block.Implementation)
	referencer, named := capability.(SecretReferencer)
	if !found || !named {
		return nil, false, authorizeRebindInvalid.Fail(
			step+" has a capability that cannot name the Secrets it references, so no Secret is rebound", remedy)
	}
	references, err := referencer.SecretReferences(block)
	return capability, slices.Contains(references, secret), err
}

func (s Service) rebind(ctx context.Context, name string, decided operationPlan) (operationPlan, error) {
	if len(decided.rebinds) == 0 {
		return decided, nil
	}
	replaced := frozenBinding(decided)
	request := custody.RebindRequest{ContextName: name, BindingID: replaced, Names: decided.rebinds}
	var issued secretstore.Binding
	var err error
	if s.startCheck != nil {
		issued, err = s.admittedRebind(ctx, request)
	} else {
		issued, err = s.binder.Rebind(ctx, request)
	}
	if err != nil {
		return operationPlan{}, err
	}
	decided.rebound = rebinding{replaced: replaced, binding: issued.ID, secrets: decided.rebinds}
	if decided.fresh {
		decided.reopen = issued.ID
	} else {
		decided.operation.Bindings = []string{issued.ID}
	}
	return decided, nil
}

type rebinding struct {
	replaced string
	binding  string
	secrets  []string
}

func (r rebinding) record() operationstore.LogRecord {
	return operationstore.LogRecord{Event: "rebind",
		Detail: "Secret binding " + r.replaced + " rebound as " + r.binding + " at the current version of " + strings.Join(r.secrets, ", ")}
}

func (s Service) admittedRebind(ctx context.Context, request custody.RebindRequest) (secretstore.Binding, error) {
	binder, ok := s.binder.(interface {
		PrepareRebind(context.Context, custody.RebindRequest) (custody.ScopedRebind, error)
	})
	if !ok {
		return secretstore.Binding{}, engineRequestDefect.Fail("the credential store cannot rebind inside the admitted transaction", "")
	}
	issue, err := binder.PrepareRebind(ctx, request)
	if err != nil {
		return secretstore.Binding{}, err
	}
	var issued secretstore.Binding
	err = s.mutateAdmitted(ctx, request.ContextName, s.startCheck, func(tx Transaction) error {
		return tx.Secrets(ctx, func(selected secretstore.Context, area secretstore.Area) error {
			var err error
			issued, err = issue(ctx, selected, area)
			return err
		})
	})
	return issued, err
}
