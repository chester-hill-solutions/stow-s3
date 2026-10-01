package policy

// Workspace is the resource a workspace operation is decided on: the collection is the
// registered workspace ID, the locator its confined relative path, so a workspace's
// identity and a path inside it are separate resources.
//
// An empty path is the root, which is what destroying one is decided on. A policy
// narrowing a workspace to a subtree does not match it, because the root is not inside
// the subtree, so a whole-workspace operation is refused not narrowed.
func Workspace(id, path string) Resource {
	return Resource{Namespace: LocalNamespace, Collection: id, Kind: KindWorkspace, Locator: path}
}
