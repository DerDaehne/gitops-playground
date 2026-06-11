package jenkins

import (
	"context"
	"fmt"
	"strings"
)

// SetGlobalProperty sets a Jenkins environment variable on the master node
// via the EnvironmentVariablesNodeProperty. Mirrors
// GlobalPropertyManager.setGlobalProperty.
func (c *Client) SetGlobalProperty(ctx context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("jenkins: SetGlobalProperty requires a key")
	}
	if err := assertNoBackslash("key", key); err != nil {
		return err
	}
	if err := assertNoBackslash("value", value); err != nil {
		return err
	}

	script := fmt.Sprintf(`
            instance = Jenkins.getInstance()
            globalNodeProperties = instance.getGlobalNodeProperties()
            envVarsNodePropertyList = globalNodeProperties.getAll(hudson.slaves.EnvironmentVariablesNodeProperty.class)

            def newEnvVarsNodeProperty
            def envVars

            if (envVarsNodePropertyList == null || envVarsNodePropertyList.size() == 0) {
                newEnvVarsNodeProperty = new hudson.slaves.EnvironmentVariablesNodeProperty()
                globalNodeProperties.add(newEnvVarsNodeProperty)
                envVars = newEnvVarsNodeProperty.getEnvVars()
            } else {
                envVars = envVarsNodePropertyList.get(0).getEnvVars()
            }

            envVars.put('%s', '%s')

            instance.save()
            print("Done")
        `, escapeGroovy(key), escapeGroovy(value))

	out, err := c.runScript(ctx, script)
	if err != nil {
		return fmt.Errorf("jenkins: SetGlobalProperty %q: %w", key, err)
	}
	out = strings.TrimSpace(out)
	if out != "Done" {
		return fmt.Errorf("jenkins: SetGlobalProperty %q returned %q", key, out)
	}
	return nil
}

// DeleteGlobalProperty removes the supplied key from the global env-vars
// node property. Returns nil if the property does not exist.
func (c *Client) DeleteGlobalProperty(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("jenkins: DeleteGlobalProperty requires a key")
	}
	if err := assertNoBackslash("key", key); err != nil {
		return err
	}
	script := fmt.Sprintf(`
            def instance = Jenkins.getInstance()
            def globalNodeProperties = instance.getGlobalNodeProperties()
            def envVarsNodePropertyList = globalNodeProperties.getAll(hudson.slaves.EnvironmentVariablesNodeProperty.class)

            if (envVarsNodePropertyList == null || envVarsNodePropertyList.size() == 0) {
                print("Nothing to do")
                return
            }

            envVars = envVarsNodePropertyList.get(0).getEnvVars()
            envVars.remove('%s')
            print("Done")
        `, escapeGroovy(key))
	out, err := c.runScript(ctx, script)
	if err != nil {
		return fmt.Errorf("jenkins: DeleteGlobalProperty %q: %w", key, err)
	}
	out = strings.TrimSpace(out)
	if out != "Done" && out != "Nothing to do" {
		return fmt.Errorf("jenkins: DeleteGlobalProperty %q returned %q", key, out)
	}
	return nil
}
