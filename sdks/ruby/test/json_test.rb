# frozen_string_literal: true

require_relative 'test_helper'

class JsonTest < Minitest::Test
  Json = ToggleSlash::Json

  def test_round_trips_every_kind
    value = { 'a' => [1, 2.5, -0.0, true, false, nil, 'x'], 'b' => { 'c' => 'd' } }
    assert_equal value, Json.parse(Json.generate(value))
  end

  def test_a_lone_surrogate_is_accepted_and_written_back
    parsed = Json.parse('"\\ud800"')
    assert_equal '"\\ud800"', Json.generate(parsed)
  end

  def test_a_surrogate_pair_is_one_character
    assert_equal "\u{1F600}", Json.parse('"\\ud83d\\ude00"')
  end

  def test_big_integers_become_floats_and_small_ones_stay_exact
    assert_kind_of Integer, Json.parse('123456789012345')
    assert_kind_of Float, Json.parse('12345678901234567890')
    assert_equal Float::INFINITY, Json.parse('1e999')
  end

  def test_what_is_not_json_raises_parse_error
    ['', '{', '[1,]', '{"a":1,}', '01', '"\\x"', "\"a\nb\"", 'nul', '[1] 2', '+1', '.5'].each do |text|
      assert_raises(Json::ParseError, text.inspect) { Json.parse(text) }
    end
  end

  def test_nesting_is_bounded
    assert_raises(Json::ParseError) { Json.parse(('[' * 5000) + (']' * 5000)) }
  end

  def test_freeze_deep_freezes_every_level
    value = Json.freeze_deep(Json.parse('{"a":[{"b":"c"}]}'))
    assert_predicate value, :frozen?
    assert_predicate value['a'], :frozen?
    assert_predicate value['a'][0], :frozen?
  end
end
