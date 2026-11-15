# frozen_string_literal: true

require "openssl"

# The ToggleSlash server SDK for Ruby: here, secure-mode signing.
module Toggleslash
  # Secure-mode signing (contract 14; the bytes are docs/sdk-browser.md, "Secure mode").
  module Sign
    CURVE = "prime256v1"
    HALF = 32

    # `<UTF-8 byte length>:<UTF-8 bytes>,`, a lone surrogate written as U+FFFD as the other SDKs and the server
    # write it.
    def self.netstring(text)
      bytes = Text.hash_bytes(text)
      "#{bytes.bytesize}:".b + bytes + ",".b
    end

    # The bytes a signature covers: the normalised context's kinds in byte order, each kind and then its key as a
    # netstring. Attributes are not signed. Raises ArgumentError for a context that is not valid.
    def self.message(context)
      contexts = Eval::Context.normalise(context)
      raise ArgumentError, "sign_context: the context is not valid; see docs/evaluation.md" if contexts.nil?

      contexts.keys.sort_by(&:b).map { |kind| netstring(kind) + netstring(contexts[kind][0]) }.join.b
    end

    def self.key(private_key_pem)
      key = begin
        OpenSSL::PKey.read(private_key_pem.to_s)
      rescue OpenSSL::PKey::PKeyError
        nil
      end
      unless key.is_a?(OpenSSL::PKey::EC) && key.private? && key.group.curve_name == CURVE
        raise ArgumentError, "sign_context: the key is not an ECDSA P-256 private key"
      end

      key
    end

    def self.sign(private_key_pem, context)
      message = message(context)
      der = key(private_key_pem).sign(OpenSSL::Digest.new("SHA256"), message)
      raw = OpenSSL::ASN1.decode(der).value.map { |part| part.value.to_s(2).rjust(HALF, "\0".b) }.join
      [raw].pack("m0").tr("+/", "-_").delete("=")
    end
  end

  # Signs a context for secure mode with an environment's signing key (the PKCS#8 PEM that
  # `POST .../environments/{env}/signing-key` answered once). It needs no client. The answer, 86 base64url
  # characters, goes to the browser with the context.
  #
  # Unlike the rest of the SDK it raises: an ArgumentError for a context that is not valid or a key that is not an
  # ECDSA P-256 private key, both mistakes at the call site (contract 14).
  #
  # @param private_key_pem [String]
  # @param context [Hash]
  # @return [String]
  def self.sign_context(private_key_pem, context)
    Sign.sign(private_key_pem, context)
  end
end
