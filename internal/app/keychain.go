package app

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/scrypt"
	"golang.org/x/term"
)

type SecretStoreKind string

const (
	SecretStoreKeychain      SecretStoreKind = "keychain"
	SecretStoreEncryptedFile SecretStoreKind = "encrypted-file"

	secretStoreModeAuto  = "auto"
	secretStoreModeFile  = "file"
	secretStoreModeEnv   = "AXIOM_CLI_SECRET_STORE"
	secretStorePassEnv   = "AXIOM_CLI_SECRET_PASSPHRASE"
	secretFileName       = "secrets.enc.json"
	secretFileVersion    = 1
	fallbackSaltSize     = 16
	fallbackDerivedBytes = 32
)

var (
	errFallbackSecretNotFound = errors.New("secret not found in encrypted file store")

	fallbackPassphraseMu sync.Mutex
	fallbackPassphrase   string
)

type encryptedSecretFile struct {
	Version    int    `json:"version"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func SaveSecret(key string, value string) (SecretStoreKind, error) {
	switch secretStoreMode() {
	case secretStoreModeFile:
		return saveFallbackSecret(key, value)
	default:
		if err := keyring.Set(KeyringServiceName(), key, value); err == nil {
			_ = deleteFallbackSecretIfExists(key)
			return SecretStoreKeychain, nil
		} else if !shouldUseFallbackStore(err) {
			return "", fmt.Errorf("save secret in keychain: %w", err)
		} else {
			storeKind, fallbackErr := saveFallbackSecret(key, value)
			if fallbackErr != nil {
				return "", fmt.Errorf("save secret in keychain: %v; fallback secret store failed: %w", err, fallbackErr)
			}
			return storeKind, nil
		}
	}
}

func LoadSecret(key string) (string, error) {
	switch secretStoreMode() {
	case secretStoreModeFile:
		return loadFallbackSecret(key)
	default:
		value, err := keyring.Get(KeyringServiceName(), key)
		if err == nil {
			return value, nil
		}

		if errors.Is(err, keyring.ErrNotFound) || shouldUseFallbackStore(err) {
			fallbackValue, fallbackErr := loadFallbackSecret(key)
			if fallbackErr == nil {
				return fallbackValue, nil
			}
			if !errors.Is(fallbackErr, errFallbackSecretNotFound) {
				return "", fallbackErr
			}
		}

		return "", fmt.Errorf("load secret from keychain: %w", err)
	}
}

func DeleteSecret(key string) error {
	switch secretStoreMode() {
	case secretStoreModeFile:
		return deleteFallbackSecret(key)
	default:
		keychainErr := deleteKeychainSecret(key)
		fallbackErr := deleteFallbackSecretIfExists(key)
		if keychainErr != nil {
			if fallbackErr != nil {
				return fmt.Errorf("delete secret from keychain: %v; fallback secret store cleanup failed: %w", keychainErr, fallbackErr)
			}
			return keychainErr
		}
		return fallbackErr
	}
}

func DeleteSecretIfExists(key string) error {
	switch secretStoreMode() {
	case secretStoreModeFile:
		return deleteFallbackSecretIfExists(key)
	default:
		keychainErr := deleteKeychainSecretIfExists(key)
		fallbackErr := deleteFallbackSecretIfExists(key)
		if keychainErr != nil {
			if fallbackErr != nil {
				return fmt.Errorf("delete secret from keychain: %v; fallback secret store cleanup failed: %w", keychainErr, fallbackErr)
			}
			return keychainErr
		}
		return fallbackErr
	}
}

func deleteKeychainSecret(key string) error {
	if err := keyring.Delete(KeyringServiceName(), key); err != nil {
		return fmt.Errorf("delete secret from keychain: %w", err)
	}
	return nil
}

func deleteKeychainSecretIfExists(key string) error {
	if _, err := keyring.Get(KeyringServiceName(), key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		if shouldUseFallbackStore(err) {
			return nil
		}
		return fmt.Errorf("load secret from keychain: %w", err)
	}

	if err := keyring.Delete(KeyringServiceName(), key); err != nil {
		return fmt.Errorf("delete secret from keychain: %w", err)
	}

	return nil
}

func secretStoreMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(secretStoreModeEnv)))
	switch mode {
	case "", secretStoreModeAuto:
		return secretStoreModeAuto
	case secretStoreModeFile:
		return secretStoreModeFile
	default:
		return "keychain"
	}
}

func shouldUseFallbackStore(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return true
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "org.freedesktop.secrets") ||
		strings.Contains(message, "secret service") ||
		strings.Contains(message, "cannot autolaunch dbus") ||
		strings.Contains(message, "no such interface") ||
		strings.Contains(message, "keychain is not available") ||
		strings.Contains(message, "credential manager")
}

func saveFallbackSecret(key string, value string) (SecretStoreKind, error) {
	path, err := secretFilePath()
	if err != nil {
		return "", err
	}

	_, statErr := os.Stat(path)
	newFile := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !newFile {
		return "", fmt.Errorf("stat encrypted secret store: %w", statErr)
	}

	passphrase, err := resolveFallbackPassphrase(newFile)
	if err != nil {
		return "", err
	}

	secrets := map[string]string{}
	if !newFile {
		secrets, err = readFallbackSecrets(passphrase)
		if err != nil {
			return "", err
		}
	}
	secrets[key] = value

	if err := writeFallbackSecrets(path, passphrase, secrets); err != nil {
		return "", err
	}

	return SecretStoreEncryptedFile, nil
}

func loadFallbackSecret(key string) (string, error) {
	passphrase, err := resolveFallbackPassphrase(false)
	if err != nil {
		return "", err
	}

	secrets, err := readFallbackSecrets(passphrase)
	if err != nil {
		return "", err
	}

	value, ok := secrets[key]
	if !ok {
		return "", errFallbackSecretNotFound
	}
	return value, nil
}

func deleteFallbackSecret(key string) error {
	passphrase, err := resolveFallbackPassphrase(false)
	if err != nil {
		return err
	}

	secrets, err := readFallbackSecrets(passphrase)
	if err != nil {
		return err
	}

	if _, ok := secrets[key]; !ok {
		return errFallbackSecretNotFound
	}
	delete(secrets, key)

	path, err := secretFilePath()
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove encrypted secret store: %w", err)
		}
		return nil
	}

	return writeFallbackSecrets(path, passphrase, secrets)
}

func deleteFallbackSecretIfExists(key string) error {
	err := deleteFallbackSecret(key)
	if err == nil || errors.Is(err, errFallbackSecretNotFound) || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func secretFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "axiom-cli", secretFileName), nil
}

func resolveFallbackPassphrase(confirm bool) (string, error) {
	if passphrase := strings.TrimSpace(os.Getenv(secretStorePassEnv)); passphrase != "" {
		return passphrase, nil
	}

	fallbackPassphraseMu.Lock()
	defer fallbackPassphraseMu.Unlock()

	if fallbackPassphrase != "" {
		return fallbackPassphrase, nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("encrypted file secret store requires %s in non-interactive environments", secretStorePassEnv)
	}

	passphrase, err := promptHidden("Axiom CLI encrypted secret-store passphrase: ")
	if err != nil {
		return "", err
	}
	if confirm {
		confirmation, confirmErr := promptHidden("Confirm passphrase: ")
		if confirmErr != nil {
			return "", confirmErr
		}
		if passphrase != confirmation {
			return "", errors.New("passphrase confirmation did not match")
		}
	}

	fallbackPassphrase = passphrase
	return passphrase, nil
}

func promptHidden(prompt string) (string, error) {
	if _, err := fmt.Fprint(os.Stderr, prompt); err != nil {
		return "", fmt.Errorf("prompt for passphrase: %w", err)
	}
	defer fmt.Fprintln(os.Stderr)

	bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}

	passphrase := strings.TrimSpace(string(bytes))
	if passphrase == "" {
		return "", errors.New("passphrase cannot be empty")
	}
	return passphrase, nil
}

func readFallbackSecrets(passphrase string) (map[string]string, error) {
	path, err := secretFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errFallbackSecretNotFound
		}
		return nil, fmt.Errorf("read encrypted secret store: %w", err)
	}

	var file encryptedSecretFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse encrypted secret store: %w", err)
	}
	if file.Version != secretFileVersion {
		return nil, fmt.Errorf("unsupported encrypted secret store version: %d", file.Version)
	}

	salt, err := base64.StdEncoding.DecodeString(file.Salt)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted secret store salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(file.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted secret store nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(file.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted secret store payload: %w", err)
	}

	key, err := deriveFallbackKey(passphrase, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create encrypted secret store cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create encrypted secret store gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt encrypted secret store: %w", err)
	}

	secrets := map[string]string{}
	if err := json.Unmarshal(plaintext, &secrets); err != nil {
		return nil, fmt.Errorf("parse encrypted secret store payload: %w", err)
	}
	return secrets, nil
}

func writeFallbackSecrets(path string, passphrase string, secrets map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create encrypted secret store directory: %w", err)
	}

	plaintext, err := json.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("marshal encrypted secret store payload: %w", err)
	}

	salt := make([]byte, fallbackSaltSize)
	if _, err := crand.Read(salt); err != nil {
		return fmt.Errorf("generate encrypted secret store salt: %w", err)
	}

	key, err := deriveFallbackKey(passphrase, salt)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("create encrypted secret store cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create encrypted secret store gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := crand.Read(nonce); err != nil {
		return fmt.Errorf("generate encrypted secret store nonce: %w", err)
	}

	file := encryptedSecretFile{
		Version:    secretFileVersion,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plaintext, nil)),
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal encrypted secret store: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write encrypted secret store: %w", err)
	}
	return nil
}

func deriveFallbackKey(passphrase string, salt []byte) ([]byte, error) {
	key, err := scrypt.Key([]byte(passphrase), salt, 1<<15, 8, 1, fallbackDerivedBytes)
	if err != nil {
		return nil, fmt.Errorf("derive encrypted secret store key: %w", err)
	}
	return key, nil
}
