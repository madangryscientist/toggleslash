# frozen_string_literal: true

require_relative "test_helper"

# sign_context (contract 14): the signed bytes from fixtures/signing, signatures that verify, and refusals.
class SignTest < Minitest::Test
  MESSAGES = File.join(TestSupport::FIXTURES, "signing", "messages.json")

  def self.messages
    Toggleslash::Json.parse(File.binread(MESSAGES))["messages"]
  end

  def pem(curve)
    OpenSSL::PKey::EC.generate(curve).private_to_pem
  end

  # The P1363 `r || s` back to the DER OpenSSL verifies.
  def verifies?(public_pem, signature, message)
    assert_match(/\A[A-Za-z0-9_-]{86}\z/, signature)
    raw = "#{signature.tr("-_", "+/")}==".unpack1("m")
    assert_equal 64, raw.bytesize
    der = OpenSSL::ASN1::Sequence.new([raw[0, 32], raw[32, 32]].map do |half|
      OpenSSL::ASN1::Integer.new(OpenSSL::BN.new(half, 2))
    end).to_der
    OpenSSL::PKey.read(public_pem).verify(OpenSSL::Digest.new("SHA256"), der, message)
  end

  def test_every_fixture_message
    messages = self.class.messages
    assert_equal 7, messages.size
    key = OpenSSL::PKey::EC.generate("prime256v1")
    messages.each do |entry|
      expected = entry["message"].b
      assert_equal expected, Toggleslash::Sign.message(entry["context"]), entry["name"]
      signature = Toggleslash.sign_context(key.private_to_pem, entry["context"])
      assert verifies?(key.public_to_pem, signature, expected), entry["name"]
    end
  end

  def test_round_trip_with_symbol_keys
    key = OpenSSL::PKey::EC.generate("prime256v1")
    signature = Toggleslash.sign_context(key.private_to_pem, { key: "user-1" })
    assert verifies?(key.public_to_pem, signature, "4:user,6:user-1,")
    refute verifies?(key.public_to_pem, signature, "4:user,6:user-2,")
  end

  def test_refuses_a_p384_key
    error = assert_raises(ArgumentError) { Toggleslash.sign_context(pem("secp384r1"), { "key" => "u" }) }
    assert_match(/P-256/, error.message)
  end

  def test_refuses_a_public_key_and_garbage
    public_pem = OpenSSL::PKey::EC.generate("prime256v1").public_to_pem
    assert_raises(ArgumentError) { Toggleslash.sign_context(public_pem, { "key" => "u" }) }
    assert_raises(ArgumentError) { Toggleslash.sign_context("not a pem", { "key" => "u" }) }
  end

  def test_refuses_an_invalid_context
    key = pem("prime256v1")
    [{}, { "key" => "" }, { "kind" => "multi", "contexts" => {} }, "user-1"].each do |context|
      assert_raises(ArgumentError) { Toggleslash.sign_context(key, context) }
    end
  end
end
