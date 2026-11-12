# frozen_string_literal: true

require_relative "test_helper"

class TextTest < Minitest::Test
  Text = Toggleslash::Text

  LONE = "\xED\xA0\xBD".dup.force_encoding(Encoding::UTF_8) # U+D83D alone

  def test_units_and_code_points
    assert_equal [0x61, 0xD83D, 0xDE00], Text.units("a\u{1F600}")
    assert_equal [0x61, 0x1F600], Text.code_points("a\u{1F600}")
    assert_equal [0xD83D], Text.units(LONE)
    assert_equal [0xD83D], Text.code_points(LONE)
  end

  def test_a_surrogate_pair_in_two_halves_joins_for_code_points
    pair = "\xED\xA0\xBD\xED\xB8\x80".dup.force_encoding(Encoding::UTF_8)
    assert_equal [0x1F600], Text.code_points(pair)
  end

  def test_hash_bytes_replace_a_lone_surrogate
    assert_equal "a\uFFFD".b, Text.hash_bytes("a#{LONE}")
  end

  def test_compare_orders_by_utf16_code_unit
    assert_equal(-1, Text.compare("\u{1F600}", "\uFFFF"))
    assert_equal 1, Text.compare("b", "a")
    assert_equal 0, Text.compare("a", "a")
  end

  def test_same_contains_starts_ends_with_lone_surrogates
    assert Text.same?(LONE, LONE.dup)
    refute Text.same?(LONE, "a")
    assert Text.contains?("x#{LONE}y", LONE)
    assert Text.starts_with?("#{LONE}y", LONE)
    assert Text.ends_with?("y#{LONE}", LONE)
    refute Text.starts_with?("y", LONE)
  end

  def test_utf8_reads_other_encodings
    assert_equal "\u00e9", Text.utf8("\xE9".dup.force_encoding(Encoding::ISO_8859_1))
    assert_equal "\u00e9", Text.utf8("\u00e9".b)
  end
end
