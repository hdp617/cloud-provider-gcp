/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kops

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EnsureStateStore ensures the GCS bucket for kOps state exists and has correct settings.
func EnsureStateStore(c *Config) error {
	if c.StateStore == "" {
		if c.GCPProject == "" {
			return fmt.Errorf("GCP_PROJECT must be set if KOPS_STATE_STORE is not provided")
		}
		c.StateStore = fmt.Sprintf("gs://kops-state-%s", c.GCPProject)
	}

	fmt.Printf("Ensuring KOPS_STATE_STORE exists: %s\n", c.StateStore)
	return ensureBucket(c.StateStore, c.GCPProject, c.GCPLocation)
}

// EnsureStagingStore ensures the GCS bucket for kOps staging exists and has correct settings.
func EnsureStagingStore(c *Config) error {
	if c.StagingStore == "" {
		if c.GCPProject == "" {
			return fmt.Errorf("GCP_PROJECT must be set if KOPS_STAGING_BUCKET is not provided")
		}
		c.StagingStore = fmt.Sprintf("gs://kops-staging-%s", c.GCPProject)
	}

	fmt.Printf("Ensuring KOPS_STAGING_BUCKET exists: %s\n", c.StagingStore)
	return ensureBucket(c.StagingStore, c.GCPProject, c.GCPLocation)
}

func ensureBucket(bucketPath, project, location string) error {
	// Check if bucket exists: gcloud storage ls --buckets --project=<project> <bucket>
	lsCmd := exec.Command("gcloud", "storage", "ls", "--buckets", "--project="+project, bucketPath)
	if err := lsCmd.Run(); err != nil {
		// Assume it doesn't exist, try to create it: gcloud storage buckets create --project=<project> --location=<location> <bucket>
		fmt.Printf("Bucket %s does not exist, creating...\n", bucketPath)
		mbCmd := exec.Command("gcloud", "storage", "buckets", "create", "--project="+project, "--location="+location, bucketPath)
		mbCmd.Stdout = os.Stdout
		mbCmd.Stderr = os.Stderr
		if err := mbCmd.Run(); err != nil {
			return fmt.Errorf("failed to create bucket: %v", err)
		}
	}

	// Disable uniform bucket-level access: gcloud storage buckets update <bucket> --no-uniform-bucket-level-access
	ublaCmd := exec.Command("gcloud", "storage", "buckets", "update", bucketPath, "--no-uniform-bucket-level-access")
	ublaCmd.Stdout = os.Stdout
	ublaCmd.Stderr = os.Stderr
	if err := ublaCmd.Run(); err != nil {
		return fmt.Errorf("failed to disable UBLA: %v", err)
	}

	// Grant storage.admin to the current account
	saCmd := exec.Command("gcloud", "config", "list", "--format", "value(core.account)")
	saBytes, err := saCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get current account: %v", err)
	}
	sa := strings.TrimSpace(string(saBytes))

	// Grant roles/storage.admin: gcloud storage buckets add-iam-policy-binding --member=<member> --role=roles/storage.admin <bucket>
	iamCmd := exec.Command("gcloud", "storage", "buckets", "add-iam-policy-binding", fmt.Sprintf("--member=serviceAccount:%s", sa), "--role=roles/storage.admin", bucketPath)
	iamCmd.Stdout = os.Stdout
	iamCmd.Stderr = os.Stderr
	if err := iamCmd.Run(); err != nil {
		fmt.Printf("Warning: failed to grant storage.admin to serviceAccount %s: %v. Retrying with user account...\n", sa, err)
		iamUserCmd := exec.Command("gcloud", "storage", "buckets", "add-iam-policy-binding", fmt.Sprintf("--member=user:%s", sa), "--role=roles/storage.admin", bucketPath)
		iamUserCmd.Stdout = os.Stdout
		iamUserCmd.Stderr = os.Stderr
		if err := iamUserCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to grant storage.admin to user %s: %v\n", sa, err)
		}
	}

	return nil
}

// EnsureSSHKey ensures that an SSH key exists for kOps.
func EnsureSSHKey(c *Config) error {
	if c.SSHPrivateKey == "" {
		return fmt.Errorf("SSHPrivateKey must be set in config")
	}

	if _, err := os.Stat(c.SSHPrivateKey); err == nil {
		fmt.Printf("SSH key already exists at %s\n", c.SSHPrivateKey)
		return nil
	}

	fmt.Printf("SSH key %s not found, creating one...\n", c.SSHPrivateKey)
	// gcloud compute --project="${GCP_PROJECT}" config-ssh --ssh-key-file="${SSH_PRIVATE_KEY}"
	cmd := exec.Command("gcloud", "compute", "--project="+c.GCPProject, "config-ssh", "--ssh-key-file="+c.SSHPrivateKey)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create SSH key: %v", err)
	}

	return nil
}

// CleanSSHKey cleanly removes SSH configuration metadata appended by kOps and deletes the generated keys.
func CleanSSHKey(c *Config) error {
	if c.SSHPrivateKey == "" {
		return nil
	}

	fmt.Printf("Cleaning up SSH configuration and keys...\n")
	cmd := exec.Command("gcloud", "compute", "--project="+c.GCPProject, "config-ssh", "--remove")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Warning: failed to cleanly remove gcloud ssh configurations: %v\n", err)
	}

	// Remove the actual key files if they exist
	_ = os.Remove(c.SSHPrivateKey)
	_ = os.Remove(c.SSHPublicKey)

	return nil
}
