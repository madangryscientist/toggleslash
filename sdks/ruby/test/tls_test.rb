# frozen_string_literal: true

require_relative "support"
require "openssl"

class TlsTest < Minitest::Test
  include Support
  include Seams

  def certificate
    key = OpenSSL::PKey::RSA.new(2048)
    cert = OpenSSL::X509::Certificate.new
    cert.version = 2
    cert.serial = 1
    cert.subject = cert.issuer = OpenSSL::X509::Name.parse("/CN=localhost")
    cert.public_key = key.public_key
    cert.not_before = Time.now - 60
    cert.not_after = Time.now + 3600
    factory = OpenSSL::X509::ExtensionFactory.new(cert, cert)
    cert.add_extension(factory.create_extension("subjectAltName", "IP:127.0.0.1,DNS:localhost"))
    cert.add_extension(factory.create_extension("basicConstraints", "CA:TRUE", true))
    cert.sign(key, OpenSSL::Digest.new("SHA256"))
    [cert, key]
  end

  def contexts
    cert, key = certificate
    serving = OpenSSL::SSL::SSLContext.new
    serving.cert = cert
    serving.key = key
    store = OpenSSL::X509::Store.new
    store.add_cert(cert)
    trusting = OpenSSL::SSL::SSLContext.new
    trusting.cert_store = store
    trusting.verify_mode = OpenSSL::SSL::VERIFY_PEER
    [serving, trusting]
  end

  def test_a_ruleset_loads_over_tls_with_a_trusted_certificate
    serving, trusting = contexts
    server = build_server(ssl_context: serving)
    server.serve_ruleset
    transport = Toggleslash::Http::Transport.new(ssl_context: trusting)
    client = seamed_client(server, transport: transport)
    assert client.ready
    assert_equal true, client.variation("on", { "key" => "u" }, false)
    assert_equal "Bearer sdk-secret-key", server.requests_to("/sdk/v1/ruleset").first.headers["authorization"]
  end

  def test_an_untrusted_certificate_is_refused_as_a_network_error
    serving, _trusting = contexts
    server = build_server(ssl_context: serving)
    server.serve_ruleset
    client = seamed_client(server)
    refute client.ready
    assert_equal "network", client.status.last_error
    assert_empty server.requests_to("/sdk/v1/ruleset")
  end

  def test_a_certificate_for_another_host_is_refused
    cert, key = certificate
    other = OpenSSL::X509::Certificate.new
    other.version = 2
    other.serial = 2
    other.subject = other.issuer = OpenSSL::X509::Name.parse("/CN=elsewhere.example")
    other.public_key = key.public_key
    other.not_before = Time.now - 60
    other.not_after = Time.now + 3600
    other.sign(key, OpenSSL::Digest.new("SHA256"))
    serving = OpenSSL::SSL::SSLContext.new
    serving.cert = other
    serving.key = key
    store = OpenSSL::X509::Store.new
    store.add_cert(other)
    trusting = OpenSSL::SSL::SSLContext.new
    trusting.cert_store = store
    trusting.verify_mode = OpenSSL::SSL::VERIFY_PEER
    server = build_server(ssl_context: serving)
    server.serve_ruleset
    client = seamed_client(server, transport: Toggleslash::Http::Transport.new(ssl_context: trusting))
    refute client.ready
    assert_equal "network", client.status.last_error
    refute_nil cert
  end
end
