# frozen_string_literal: true

class ConnectIdentity::UsersController < ConnectIdentity::BaseController
  def show
    user = User.find_by(id: params[:id])
    return head :not_found if user.blank?

    render_minimal_user(user)
  end
end
