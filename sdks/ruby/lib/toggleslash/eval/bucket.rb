# frozen_string_literal: true

require 'digest'
require_relative 'text'

module ToggleSlash
  module Eval
    # Buckets run 0..99999; a rollout weight of 1000 is 1%.
    BUCKET_COUNT = 100_000

    # Where a context lands for a flag or segment: SHA-256 of the UTF-8 bytes of
    # "<key>.<salt>.<contextKey>", the first 4 bytes as a big-endian uint32, modulo 100000.
    module Bucket
      DOT = '.'.b.freeze

      module_function

      def uint32(key, salt, context_key)
        input = Text.hash_bytes(key) + DOT + Text.hash_bytes(salt) + DOT + Text.hash_bytes(context_key)
        Digest::SHA256.digest(input).unpack1('N')
      end

      def bucket(key, salt, context_key)
        uint32(key, salt, context_key) % BUCKET_COUNT
      end
    end
  end
end
