package mail

import "context"

func (c *IMAPClient) FolderExists(ctx context.Context, name string) (bool, error) {
	folders, err := c.ListFolders(ctx)
	if err != nil {
		return false, err
	}
	for _, f := range folders {
		if f.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (c *IMAPClient) EnsureFolder(ctx context.Context, name string) error {
	exists, err := c.FolderExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return c.CreateFolder(ctx, name)
	}
	return nil
}
