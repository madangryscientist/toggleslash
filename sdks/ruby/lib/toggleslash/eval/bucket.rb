# frozen_string_literal: true

require "openssl"

module Toggleslash
  module Eval
    # Bucketing: the first four bytes of SHA-256 over "<key>.<salt>.<contextKey>", modulo 100000.
    module Bucket
      COUNT = 100_000

      def self.uint32(key, salt, context_key)
        input = Text.hash_bytes(key) + ".".b + Text.hash_bytes(salt) + ".".b + Text.hash_bytes(context_key)
        OpenSSL::Digest::SHA256.digest(input).unpack1("N")
      end

      def self.bucket(key, salt, context_key)
        uint32(key, salt, context_key) % COUNT
      end
    end
  end
end
