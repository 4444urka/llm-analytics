import React from "react";

export interface ICardProps {
  children?: React.ReactNode;
}

const Card: React.FC<ICardProps> = ({ children }) => {
  return <div className="card">{children}</div>;
};

export default Card;
