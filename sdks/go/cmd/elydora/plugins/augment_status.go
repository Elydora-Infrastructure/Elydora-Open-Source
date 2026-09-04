package plugins

import (
	"bytes"
	"path/filepath"
	"sort"
)

func managedAugmentIDs(
	groups []augmentGroup,
	wrapperName string,
	runtimeRoot string,
) map[string]string {
	result := map[string]string{}
	for _, group := range groups {
		for _, handler := range group.handlers {
			agentID, managed := managedAugmentAgentID(handler, wrapperName, runtimeRoot)
			if managed {
				result[managedReferenceKey(agentID)] = agentID
			}
		}
	}
	return result
}

func augmentRuntimeContracts(
	hooks augmentHooks,
	runtimeRoot string,
) []augmentRuntimeContract {
	guards := managedAugmentIDs(hooks["PreToolUse"], augmentGuardWrapperName(), runtimeRoot)
	audits := managedAugmentIDs(hooks["PostToolUse"], augmentAuditWrapperName(), runtimeRoot)
	keys := make([]string, 0, len(guards))
	for key := range guards {
		if _, exists := audits[key]; exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	contracts := make([]augmentRuntimeContract, 0, len(keys))
	for _, key := range keys {
		agentID := guards[key]
		agentDirectory := filepath.Join(runtimeRoot, agentID)
		wrappers := resolveAugmentWrapperPaths(agentDirectory)
		contracts = append(contracts, augmentRuntimeContract{
			agentID:      agentID,
			guardPath:    filepath.Join(agentDirectory, augmentGuardScript),
			auditPath:    filepath.Join(agentDirectory, augmentAuditScript),
			guardWrapper: wrappers.guard,
			auditWrapper: wrappers.audit,
		})
	}
	return contracts
}

func augmentWrappersMatch(contract augmentRuntimeContract, nodePath string) (bool, error) {
	wrappers := resolveAugmentWrapperPaths(filepath.Dir(contract.guardPath))
	if !sameManagedPath(contract.guardWrapper, wrappers.guard) ||
		!sameManagedPath(contract.auditWrapper, wrappers.audit) {
		return false, nil
	}
	for _, item := range []struct {
		path, label string
		expected    []byte
	}{
		{contract.guardWrapper, "Auggie guard wrapper", buildAugmentWrapper(nodePath, contract.guardPath)},
		{contract.auditWrapper, "Auggie audit wrapper", buildAugmentWrapper(nodePath, contract.auditPath)},
	} {
		snapshot, err := readManagedFile(item.path, item.label, maxManagedSourceBytes)
		if err != nil || snapshot == nil || !bytes.Equal(snapshot.contents, item.expected) {
			return false, err
		}
	}
	return true, nil
}

func augmentRuntimeFilesExist(
	contracts []augmentRuntimeContract,
	runtimeRoot string,
) (bool, error) {
	nodePath, err := resolveAbsoluteNodeRuntime("Auggie")
	if err != nil {
		return false, err
	}
	for _, contract := range contracts {
		runtime := managedRuntimeContract{contract.agentID, contract.guardPath, contract.auditPath}
		exists, err := managedRuntimeContractExists(runtime, augmentAgentKey, "Auggie", nil)
		if err != nil || !exists {
			if err != nil {
				return false, err
			}
			continue
		}
		matches, err := augmentWrappersMatch(contract, nodePath)
		if err != nil || matches {
			return matches, err
		}
	}
	return false, nil
}
